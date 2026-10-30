package graalgo.p3net;

import java.io.IOException;
import java.nio.channels.CancelledKeyException;
import java.nio.channels.ClosedChannelException;
import java.nio.channels.ClosedSelectorException;
import java.nio.channels.DatagramChannel;
import java.nio.channels.SelectableChannel;
import java.nio.channels.SelectionKey;
import java.nio.channels.Selector;
import java.nio.channels.ServerSocketChannel;
import java.nio.channels.SocketChannel;
import java.util.Iterator;
import java.util.Set;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.FutureTask;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.jfr.ParkReason;
import graalgo.p3os.GoOs;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

/**
 * Host-side implementation of the {@code internal/poll.runtime_poll*}
 * linkname intrinsics (P3-M9, RISK-M9-1): the netpoller.
 *
 * <p>Contract (gc {@code runtime/netpoll.go}, adapted to single-result
 * linkname declarations — the v1 frontend rejects multi-result
 * declarations, so the fork declares
 * {@code func runtime_pollOpen(fd uintptr) uintptr} returning the
 * pollDesc cookie, 0 on failure, with the errno available from
 * {@code internal/poll.runtime_pollOpenErrno}):
 * <ul>
 *   <li>{@code runtime_pollServerInit()} — idempotent poller startup.
 *   <li>{@code runtime_pollOpen(fd uintptr) uintptr} — registers the
 *       socket cookie's channel with the poller; returns the pollDesc
 *       cookie (0 on failure; errno via {@code runtime_pollOpenErrno}).
 *   <li>{@code runtime_pollClose(ctx uintptr)} — deregisters; requires a
 *       prior {@code runtime_pollUnblock} (gc: "close polldesc w/o
 *       unblock") and no parked waiters. Does NOT close the channel —
 *       the fd close is a separate {@code graalgo.os.close}, gc parity.
 *   <li>{@code runtime_pollWait(ctx uintptr, mode int) int} — parks the
 *       calling vthread until the fd is ready (0), closing (1), deadline
 *       (2), or not-pollable (3); codes match
 *       {@code internal/poll/fd_poll_runtime.go}.
 *   <li>{@code runtime_pollWaitCanceled(ctx uintptr, mode int)} — like
 *       wait but ignores deadlines (windows-only in gc; kept for
 *       contract completeness).
 *   <li>{@code runtime_pollReset(ctx uintptr, mode int) int} — pre-wait
 *       check + disarm of stale interest.
 *   <li>{@code runtime_pollSetDeadline(ctx uintptr, d int64, mode int)} —
 *       d is relative nanoseconds (per {@code fd_poll_runtime.go}:
 *       {@code d = int64(time.Until(t))}); d &gt; 0 arms, d == 0 disarms,
 *       d &lt; 0 expires immediately. Wired into the Phase-2
 *       {@link graalgo.runtime.TimerHeap} via {@code addCallback}.
 *   <li>{@code runtime_pollUnblock(ctx uintptr)} — marks closing, wakes
 *       both waiters with {@code pollErrClosing}, stops deadline timers.
 *   <li>{@code runtime_isPollServerDescriptor(fd uintptr) bool} —
 *       always false (Go never sees the poller's selector fd).
 * </ul>
 *
 * <p>Scheduler integration (the RISK-M9-1 core): the poller is a single
 * dedicated daemon platform thread driving a JDK {@link Selector}
 * (epoll on Linux) — <em>not</em> a GMP carrier, so no carrier carve-out
 * is needed (experiment E1's design half). {@code runtime_pollWait}
 * parks via {@link SchedulerCore#parkCurrent} (releasing the in-flight
 * slot, like every other park — a thousand parked connections consume
 * no carriers), and every wakeup re-enters through
 * {@link SchedulerCore#unpark} → the drainer's priority WAKE lane.
 *
 * <p>Exactly-once wakeup discipline: each waiter slot holds a
 * {@link Waiter} whose {@code result} is an
 * {@code AtomicReference<Integer>} (null = pending). Readiness (poller
 * thread), deadline expiry (timer thread) and close/unblock (any
 * thread) race to CAS it; <strong>only the CAS winner unparks</strong>.
 * This structurally prevents stale {@code UNPARK}s: the drainer's
 * {@code unparkPending} stash would otherwise poison the goroutine's
 * <em>next</em> park with a spurious immediate wakeup.
 *
 * <p>Level-triggered semantics: interest bits are set before parking
 * and cleared by the CAS winner. A readiness that arrives between the
 * last select and the park is still observed (level-triggered), and a
 * readiness that arrives before the park is absorbed by the drainer's
 * unpark-before-parked stash — no wakeup is lost in either order.
 *
 * <p>Like {@link GoOs}, state is per-program-run; {@link #resetForRun}
 * tears it down at every program boundary (called by the launcher
 * before {@code GoOs.init}; it closes the selector, stopping the poller
 * thread, and the next {@code runtime_pollServerInit} starts a fresh
 * one). {@link #resetForTest} is the test alias of the same teardown.
 */
public final class GoNetpoll {
    private GoNetpoll() {}

    // Result codes — must match internal/poll/fd_poll_runtime.go.
    public static final int POLL_NO_ERROR = 0;
    public static final int POLL_ERR_CLOSING = 1;
    public static final int POLL_ERR_TIMEOUT = 2;
    public static final int POLL_ERR_NOT_POLLABLE = 3;

    // Modes ('r', 'w', 'r'+'w').
    static final int MODE_READ = 'r';
    static final int MODE_WRITE = 'w';
    static final int MODE_RW = 'r' + 'w';

    /** One parked wait: the handle plus the single-assignment result. */
    private static final class Waiter {
        final GoroutineHandle handle;
        /**
         * null = claimed but not yet committed; an Integer 0..3 = the
         * wakeup result (CAS winner); WITHDRAWN = the waiter withdrew
         * before parking (close/deadline won the pre-park re-check).
         * Exactly one of "waker CASes a result" / "waiter CASes
         * WITHDRAWN" wins — so a waker never unparks a goroutine that
         * decided not to park (no stale UNPARK leaks into its next
         * park), and a waiter never parks after a waker gave up on it
         * (no lost wakeup).
         */
        final AtomicReference<Integer> result = new AtomicReference<>();
        /**
         * Test hook: nanotime when this waiter won its wakeup CAS
         * (poller/timer/close side, before unpark). Lets tests measure
         * the wakeup path without waiter-side carrier-scheduling noise.
         */
        volatile long wakeNanos;

        Waiter(GoroutineHandle handle) {
            this.handle = handle;
        }
    }

    /** Sentinel result: the waiter withdrew before parking. */
    private static final Integer WITHDRAWN = Integer.valueOf(-1);

    /** The pollDesc: one per registered fd. */
    private static final class PollDesc {
        final long id;
        final long sockCookie;
        /**
         * The registered channel. Volatile + non-final: the fork calls
         * {@code runtime_pollOpen} before {@code listen()}/{@code connect()}
         * swap the channel, so {@link #migrateSocketChannel} re-points it
         * (P0-1). Always the channel {@link #key} was registered for.
         */
        volatile SelectableChannel ch;
        /**
         * True for ServerSocketChannel (accept readiness = OP_ACCEPT).
         * Volatile: re-read from live state on every wait entry (P0-2),
         * updated by {@link #migrateSocketChannel}.
         */
        volatile boolean listener;
        /** Set while a nonblocking connect is in flight (OP_CONNECT). */
        volatile boolean connecting;
        volatile SelectionKey key;
        final AtomicReference<Waiter> reader = new AtomicReference<>();
        final AtomicReference<Waiter> writer = new AtomicReference<>();
        volatile boolean closing;
        volatile boolean closed;
        /** Absolute nanotime deadlines; 0 = none, < 0 = expired. */
        volatile long readDeadline;
        volatile long writeDeadline;
        /**
         * Armed timer ids (-1 = none). Atomic: the deadline callback
         * claims the id with CAS, so a stale callback can never clobber
         * a re-armed timer's id (which would both leak the new timer
         * and deliver a spurious TIMEOUT).
         */
        final AtomicLong readTimerId = new AtomicLong(-1);
        final AtomicLong writeTimerId = new AtomicLong(-1);
        /** Last completed waiter (test hook for wakeup-latency measurement). */
        volatile Waiter lastDone;

        PollDesc(long id, long sockCookie, SelectableChannel ch, boolean listener) {
            this.id = id;
            this.sockCookie = sockCookie;
            this.ch = ch;
            this.listener = listener;
        }
    }

    // ---- process-wide poller state (per program run) ----

    private static final AtomicBoolean INIT = new AtomicBoolean(false);
    private static volatile Selector selector;
    private static volatile Thread pollerThread;
    private static final ConcurrentHashMap<Long, PollDesc> DESCS = new ConcurrentHashMap<>();
    private static final ConcurrentHashMap<Long, PollDesc> BY_SOCK = new ConcurrentHashMap<>();
    private static final AtomicLong NEXT_CTX = new AtomicLong(1); // 0 = null ctx
    private static final BlockingQueue<FutureTask<SelectionKey>> REG_Q =
            new LinkedBlockingQueue<>();
    private static final ThreadLocal<Integer> LAST_OPEN_ERRNO = new ThreadLocal<>();
    /** Last poller-thread failure, for tests (no stdout/stderr in library code). */
    private static volatile Throwable lastPollerError;

    /**
     * Production per-run teardown (P1-5, independent review 2026-10-30):
     * drops every pollDesc, cancels every selector key, closes the
     * selector, and stops the poller thread, so a previous program's
     * unclosed sockets (panic / os.Exit / fatal exit, which never run
     * {@code runtime_pollClose}) cannot leak tables, hold channels, or
     * keep waking for dead channels into the next program — which
     * reuses cookie numbers from 16 (see {@code GoOs.init}).
     *
     * <p>Precondition: called on the launcher thread at a program
     * boundary, when no guest goroutine is running (the launcher awaits
     * quiescence before returning, so no waiter is parked; a waiter
     * stranded by an abnormal exit is already unresumable — its
     * scheduler is gone — and this reset deliberately does NOT unpark
     * it into the next run's scheduler, which would enqueue an UNPARK
     * token for a dead handle). Stale deadline timers are defused
     * without touching the scheduler: their ids are reset to -1, so a
     * late callback's CAS in {@link #onDeadline} fails and it is
     * ignored (the timer-heap entry itself is a no-op when it fires).
     *
     * <p>The old poller thread is joined (bounded: 1 s) before
     * returning, so a fresh {@code runtime_pollServerInit} can never
     * share {@link #REG_Q} with a still-draining predecessor that
     * would register the new run's channels on the closed selector.
     * A thread that fails to exit in the bound is left as a daemon;
     * its selector is closed, so it cannot register anything new.
     */
    public static synchronized void resetForRun() {
        INIT.set(false);
        Selector sel = selector;
        Thread oldPoller = pollerThread;
        selector = null;
        pollerThread = null;
        // Mark closed first: a poller iteration already in flight
        // drops the desc via its DESCS/closed guards instead of
        // waking a waiter for it.
        for (PollDesc pd : DESCS.values()) {
            synchronized (pd) {
                pd.closing = true;
                pd.closed = true;
            }
            // Defuse stale deadline timers (see class doc above).
            pd.readTimerId.set(-1);
            pd.writeTimerId.set(-1);
            pd.readDeadline = 0;
            pd.writeDeadline = 0;
            SelectionKey key = pd.key;
            if (key != null) {
                try {
                    key.cancel();
                } catch (RuntimeException ignored) {
                    // Already cancelled; the desc is dropped below anyway.
                }
            }
        }
        DESCS.clear();
        BY_SOCK.clear();
        REG_Q.clear();
        NEXT_CTX.set(1);
        lastPollerError = null;
        if (sel != null && sel.isOpen()) {
            try {
                sel.close();
            } catch (IOException ignored) {
                // Best effort; the poller thread exits on ClosedSelectorException.
            }
        }
        if (oldPoller != null && oldPoller != Thread.currentThread()) {
            try {
                oldPoller.join(1000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
    }

    /** Test hook: the {@link #resetForRun} teardown (same semantics). */
    public static void resetForTest() {
        resetForRun();
    }

    /** Test hook: the last unexpected poller-thread failure, if any. */
    public static Throwable lastPollerError() {
        return lastPollerError;
    }

    /**
     * Test hook: run on the waiter thread after the pre-wait
     * {@code checkErr} passes but before the waiter slot is claimed.
     * Lets tests force the close-vs-wait interleaving deterministically
     * (unblock landing between check and claim must not park forever).
     * Null in production.
     */
    static volatile Runnable preClaimHookForTest;

    /** Number of registered pollDescs (tests). */
    static int descCount() {
        return DESCS.size();
    }

    // ------------------------------------------------------------------
    // Linkname targets.
    // ------------------------------------------------------------------

    /** internal/poll.runtime_pollServerInit */
    public static void pollServerInit() {
        if (INIT.compareAndSet(false, true)) {
            try {
                Selector sel = Selector.open();
                selector = sel;
                // The thread captures its own selector: it must not
                // re-read the static, which resetForRun() may null
                // (a slow-to-start thread would otherwise NPE-spin).
                Thread t = Thread.ofPlatform().daemon(true).name("graalgo-netpoll")
                        .start(() -> pollLoop(sel));
                t.setPriority(Thread.MAX_PRIORITY);
                pollerThread = t;
            } catch (IOException e) {
                INIT.set(false);
                throw new IllegalStateException("graalgo-netpoll: Selector.open failed", e);
            }
        }
    }

    /** internal/poll.runtime_pollOpen — ctx cookie, 0 on failure. */
    public static long pollOpen(Object fdArg) {
        pollServerInit();
        long cookie = ((Number) fdArg).longValue();
        SelectableChannel ch = GoNet.selectableChannel(cookie);
        if (ch == null) {
            LAST_OPEN_ERRNO.set(GoNet.ENOTSOCK);
            return 0;
        }
        boolean listener = ch instanceof ServerSocketChannel;
        long id = NEXT_CTX.getAndIncrement();
        PollDesc pd = new PollDesc(id, cookie, ch, listener);
        GoNet.SockState st = GoNet.sockState(cookie);
        if (st != null && st.kind == GoNet.SockStateKind.CONNECTING) {
            pd.connecting = true;
        }
        DESCS.put(id, pd);
        BY_SOCK.put(cookie, pd);
        // Registration must happen on the selector thread: enqueue + wakeup,
        // block (briefly, carrier-pinned like gc's synchronous netpollopen).
        FutureTask<SelectionKey> task = new FutureTask<>(() -> ch.register(selector, 0, pd));
        REG_Q.offer(task);
        Selector sel = selector;
        if (sel == null) {
            // A program-boundary resetForRun() raced us (guest code
            // running concurrently with a boundary reset is a launcher
            // bug, not a guest error — but fail loud, never hang).
            dropDesc(pd);
            LAST_OPEN_ERRNO.set(GoOs.EINVAL);
            return 0;
        }
        sel.wakeup();
        try {
            pd.key = task.get();
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            // Cancel the registration task before dropping the desc: it
            // may otherwise run later on the poller thread and register
            // the (still-open) channel, stranding it until the handleKey
            // unknown-desc guard cancels it. Cancelling here keeps the
            // failure synchronous and local (P2-7).
            task.cancel(false);
            dropDesc(pd);
            LAST_OPEN_ERRNO.set(GoOs.EINVAL);
            return 0;
        } catch (Exception e) {
            dropDesc(pd);
            LAST_OPEN_ERRNO.set(GoNet.sockErrno(e.getCause() != null ? e.getCause() : e));
            return 0;
        }
        LAST_OPEN_ERRNO.set(0);
        return id;
    }

    /** internal/poll.runtime_pollOpenErrno — errno of the last pollOpen on this thread. */
    public static long pollOpenErrno() {
        Integer e = LAST_OPEN_ERRNO.get();
        return e == null ? 0 : e;
    }

    /** internal/poll.runtime_pollClose */
    public static void pollClose(Object ctxArg) {
        PollDesc pd = requireDesc(((Number) ctxArg).longValue());
        synchronized (pd) {
            if (!pd.closing) {
                throw new IllegalStateException("runtime: close polldesc w/o unblock");
            }
            // gc parity (pdReady): the fork calls runtime_pollUnblock then
            // runtime_pollClose back-to-back, so a waiter woken by unblock
            // usually has not been resumed by the drainer yet. Throw only
            // for a waiter that is genuinely still parked — i.e. whose
            // result CAS is still unclaimed (P0-3). A woken waiter
            // (result != null) resumes, observes CLOSING, and clears its
            // own slot; it holds pd directly and never re-resolves DESCS.
            Waiter r = pd.reader.get();
            if (r != null && r.result.get() == null) {
                throw new IllegalStateException("runtime: blocked read on closing polldesc");
            }
            Waiter w = pd.writer.get();
            if (w != null && w.result.get() == null) {
                throw new IllegalStateException("runtime: blocked write on closing polldesc");
            }
            pd.closed = true;
        }
        cancelDeadline(pd, MODE_READ);
        cancelDeadline(pd, MODE_WRITE);
        SelectionKey key = pd.key;
        if (key != null) {
            key.cancel();
        }
        dropDesc(pd);
        Selector sel = selector;
        if (sel != null) {
            sel.wakeup();
        }
    }

    /** internal/poll.runtime_pollWait — parks until ready/closing/timeout/not-pollable. */
    public static long pollWait(Object ctxArg, Object modeArg) {
        return pollWaitRaw(((Number) ctxArg).longValue(), ((Number) modeArg).intValue(),
                false);
    }

    /** internal/poll.runtime_pollWaitCanceled — like wait, ignoring deadlines. */
    public static void pollWaitCanceled(Object ctxArg, Object modeArg) {
        // gc's waitio=true ignores deadlines AND closing, re-parking until
        // readiness (windows-only in gc; the fork never calls this on
        // linux). With timeouts ignored the result is always NO_ERROR or
        // CLOSING — NOT_POLLABLE is unreachable (no eventErr state, P2-1)
        // — so a re-park loop would hot-spin on CLOSING, which pollWaitRaw
        // reports immediately without parking. Return the raw result (P2-3).
        pollWaitRaw(((Number) ctxArg).longValue(), ((Number) modeArg).intValue(), true);
    }

    /** internal/poll.runtime_pollReset */
    public static long pollReset(Object ctxArg, Object modeArg) {
        PollDesc pd = requireDesc(((Number) ctxArg).longValue());
        int mode = ((Number) modeArg).intValue();
        checkMode(mode);
        refreshChannelState(pd); // P0-2: live connecting/listener, not the pollOpen snapshot
        int err = checkErr(pd, mode);
        if (err != POLL_NO_ERROR) {
            return err;
        }
        clearInterest(pd, mode);
        return POLL_NO_ERROR;
    }

    /** internal/poll.runtime_pollSetDeadline — d is relative nanoseconds. */
    public static void pollSetDeadline(Object ctxArg, Object dArg, Object modeArg) {
        PollDesc pd = requireDesc(((Number) ctxArg).longValue());
        long d = ((Number) dArg).longValue();
        int mode = ((Number) modeArg).intValue();
        boolean read = mode == MODE_READ || mode == MODE_RW;
        boolean write = mode == MODE_WRITE || mode == MODE_RW;
        if (read) {
            armDeadline(pd, MODE_READ, d);
        }
        if (write) {
            armDeadline(pd, MODE_WRITE, d);
        }
    }

    /** internal/poll.runtime_pollUnblock */
    public static void pollUnblock(Object ctxArg) {
        PollDesc pd = requireDesc(((Number) ctxArg).longValue());
        synchronized (pd) {
            if (pd.closing) {
                throw new IllegalStateException("runtime: unblock on closing polldesc");
            }
            pd.closing = true;
        }
        cancelDeadline(pd, MODE_READ);
        cancelDeadline(pd, MODE_WRITE);
        wakeWith(pd, MODE_READ, POLL_ERR_CLOSING);
        wakeWith(pd, MODE_WRITE, POLL_ERR_CLOSING);
    }

    /** internal/poll.runtime_isPollServerDescriptor — always false (fd is not visible to Go). */
    public static boolean isPollServerDescriptor(Object fdArg) {
        return false;
    }

    /**
     * Safety net for closes that bypass {@code runtime_pollClose}
     * (e.g. {@code os.NewFile} over a socket): idempotent unblock +
     * deregister. Called by {@code GoOs.close} for {@code KIND_SOCKET}.
     */
    public static void evictForClose(long sockCookie) {
        PollDesc pd = BY_SOCK.get(sockCookie);
        if (pd == null) {
            return;
        }
        synchronized (pd) {
            pd.closing = true;
            pd.closed = true;
        }
        cancelDeadline(pd, MODE_READ);
        cancelDeadline(pd, MODE_WRITE);
        wakeWith(pd, MODE_READ, POLL_ERR_CLOSING);
        wakeWith(pd, MODE_WRITE, POLL_ERR_CLOSING);
        SelectionKey key = pd.key;
        if (key != null) {
            key.cancel();
        }
        dropDesc(pd);
        Selector sel = selector;
        if (sel != null) {
            sel.wakeup();
        }
    }

    // ------------------------------------------------------------------
    // Channel migration (P0-1) + live channel-state refresh (P0-2).
    // ------------------------------------------------------------------

    /**
     * Re-reads channel-derived pollDesc state from the live socket state.
     * The fork calls {@code runtime_pollOpen} in {@code FD.Init}, before
     * {@code connect()}/{@code listen()} run, so the snapshot taken in
     * {@code pollOpen} is stale by the first wait: {@code connecting} must
     * be true while a nonblocking connect is in flight (so
     * {@code setInterest} arms OP_CONNECT, not OP_WRITE), and
     * {@code listener} must track the current channel. Called on every
     * wait entry ({@code pollWaitRaw}, {@code pollReset}).
     *
     * <p>NOTE: this fixes the <em>flags</em> only. A swapped channel
     * (listen/connect-retry path) additionally needs selector
     * re-registration — see {@link #migrateSocketChannel}, which the
     * swap sites call. Every {@code GoOs.replaceSocketChannel} must be
     * followed by a {@code migrateSocketChannel}; the wait path stays
     * non-blocking and a missed migration fails loud (dead key), not
     * silent.
     */
    static void refreshChannelState(PollDesc pd) {
        GoNet.SockState st = GoNet.sockState(pd.sockCookie);
        pd.connecting = st != null && st.kind == GoNet.SockStateKind.CONNECTING;
        SelectableChannel cur = GoNet.selectableChannel(pd.sockCookie);
        if (cur != null) {
            pd.listener = cur instanceof ServerSocketChannel;
        }
    }

    /**
     * Moves a pollDesc's selector registration to the socket's current
     * channel. Called by {@code GoNet.listen()} after it swaps the idle
     * {@code SocketChannel} for a {@code ServerSocketChannel}, and by
     * {@code GoNet.connect()}'s CONNECT_ERROR retry path after its swap:
     * in both cases the fork already ran {@code runtime_pollOpen} on the
     * old channel (FD.Init order), and the old channel is closed by the
     * swap — without migration the pollDesc would watch a dead key and
     * every wait would park forever (P0-1).
     *
     * <p>No-op when the cookie has no pollDesc, the desc is closed, or
     * the channel is unchanged. Returns 0 or -errno (mirroring the trap
     * conventions); on registration failure the desc is dropped so the
     * failure is loud (unknown ctx) rather than a silent hang.
     */
    public static long migrateSocketChannel(long sockCookie) {
        PollDesc pd = BY_SOCK.get(sockCookie);
        if (pd == null) {
            return 0;
        }
        synchronized (pd) {
            if (pd.closed || !DESCS.containsKey(pd.id)) {
                return 0;
            }
            SelectableChannel cur = GoNet.selectableChannel(sockCookie);
            if (cur == null || cur == pd.ch) {
                return 0;
            }
            SelectionKey oldKey = pd.key;
            if (oldKey != null) {
                oldKey.cancel();
            }
            // Registration must happen on the selector thread (same
            // FutureTask pattern as pollOpen).
            FutureTask<SelectionKey> task = new FutureTask<>(() -> cur.register(selector, 0, pd));
            REG_Q.offer(task);
            Selector sel = selector;
            if (sel == null) {
                // A program-boundary resetForRun() raced us (see pollOpen).
                dropDesc(pd);
                return -GoOs.EINVAL;
            }
            sel.wakeup();
            try {
                pd.key = task.get();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                task.cancel(false); // P2-7: never strand a registration
                dropDesc(pd);
                return -GoOs.EINVAL;
            } catch (Exception e) {
                dropDesc(pd);
                return -GoNet.sockErrno(e.getCause() != null ? e.getCause() : e);
            }
            pd.ch = cur;
            pd.listener = cur instanceof ServerSocketChannel;
            // Re-arm interest for claimed-but-unresolved waiter slots;
            // level-triggering makes this lossless.
            rearmClaimed(pd);
            return 0;
        }
    }

    /** Re-applies interest for waiter slots claimed but not yet resolved. */
    private static void rearmClaimed(PollDesc pd) {
        Waiter r = pd.reader.get();
        if (r != null && r.result.get() == null) {
            setInterest(pd, MODE_READ, true);
        }
        Waiter w = pd.writer.get();
        if (w != null && w.result.get() == null) {
            setInterest(pd, MODE_WRITE, true);
        }
    }

    // ------------------------------------------------------------------
    // Wait mechanics.
    // ------------------------------------------------------------------

    private static PollDesc requireDesc(long ctx) {
        PollDesc pd = DESCS.get(ctx);
        if (pd == null) {
            throw new IllegalStateException(
                    "graalgo-netpoll: unknown pollDesc ctx " + ctx);
        }
        return pd;
    }

    private static void dropDesc(PollDesc pd) {
        DESCS.remove(pd.id);
        BY_SOCK.remove(pd.sockCookie, pd);
    }

    private static void checkMode(int mode) {
        if (mode != MODE_READ && mode != MODE_WRITE) {
            throw new IllegalArgumentException(
                    "graalgo-netpoll: bad mode " + (int) mode);
        }
    }

    private static AtomicReference<Waiter> slot(PollDesc pd, int mode) {
        return mode == MODE_READ ? pd.reader : pd.writer;
    }

    /** gc netpollcheckerr parity. */
    private static int checkErr(PollDesc pd, int mode) {
        if (pd.closing) {
            return POLL_ERR_CLOSING;
        }
        long now = System.nanoTime();
        if (mode == MODE_READ && isExpired(pd.readDeadline, now)) {
            return POLL_ERR_TIMEOUT;
        }
        if (mode == MODE_WRITE && isExpired(pd.writeDeadline, now)) {
            return POLL_ERR_TIMEOUT;
        }
        // No eventErr state (P2-1): real errors surface through the channel
        // operations themselves; synthesizing NOT_POLLABLE here was a
        // false-positive risk, so the field and both branches are gone.
        // POLL_ERR_NOT_POLLABLE stays a defined code (contract parity with
        // internal/poll/fd_poll_runtime.go) but is never produced.
        return POLL_NO_ERROR;
    }

    private static boolean isExpired(long deadline, long now) {
        return deadline != 0 && (deadline < 0 || deadline <= now);
    }

    private static int pollWaitRaw(long ctx, int mode, boolean ignoreTimeout) {
        PollDesc pd = requireDesc(ctx);
        checkMode(mode);
        // P0-2: the fork runs runtime_pollOpen in FD.Init, BEFORE
        // connect()/listen(). The connecting/listener snapshot taken in
        // pollOpen is stale by the first wait — re-read live state here
        // (and in pollReset) so setInterest arms OP_CONNECT/OP_ACCEPT
        // from the truth, not the snapshot.
        refreshChannelState(pd);
        int err = ignoreTimeout ? checkErrNoTimeout(pd, mode) : checkErr(pd, mode);
        if (err != POLL_NO_ERROR) {
            return err;
        }
        Runnable hook = preClaimHookForTest;
        if (hook != null) {
            hook.run();
        }
        SchedulerCore sched = SchedulerCore.current();
        Waiter w = new Waiter(sched.currentGoroutine());
        if (!slot(pd, mode).compareAndSet(null, w)) {
            throw new IllegalStateException("runtime: concurrent poll wait on one polldesc");
        }
        setInterest(pd, mode, true);
        // Re-check after claiming the slot: closing or a deadline may
        // have been set between the first checkErr and the claim (gc's
        // netpollblock re-checks the same way). If the re-check fails,
        // withdraw — but only if no waker has claimed us in the
        // meantime (result CAS); if a waker won, we must park and
        // consume its wakeup.
        err = ignoreTimeout ? checkErrNoTimeout(pd, mode) : checkErr(pd, mode);
        if (err != POLL_NO_ERROR && w.result.compareAndSet(null, WITHDRAWN)) {
            slot(pd, mode).compareAndSet(w, null);
            clearInterest(pd, mode);
            return err;
        }
        // The park releases the in-flight slot; wakeup re-enters via the
        // drainer's WAKE lane (unpark). The result CAS above/below is the
        // single linearization point: exactly one of withdraw / wakeup
        // wins, so the result is always set on resume.
        sched.parkCurrent(() -> {}, ParkReason.NETPOLL);
        Integer res = w.result.get();
        slot(pd, mode).compareAndSet(w, null);
        pd.lastDone = w;
        if (res == null || res == WITHDRAWN) {
            throw new AssertionError("graalgo-netpoll: wait resumed with no result");
        }
        return res;
    }

    private static int checkErrNoTimeout(PollDesc pd, int mode) {
        if (pd.closing) {
            return POLL_ERR_CLOSING;
        }
        return POLL_NO_ERROR;
    }

    /**
     * CAS-winner wakeup: exactly one of readiness/deadline/close wins;
     * only the winner clears interest and unparks (no stale UNPARK can
     * leak into the goroutine's next park).
     */
    private static void wakeWith(PollDesc pd, int mode, int result) {
        Waiter w = slot(pd, mode).get();
        if (w != null && w.result.compareAndSet(null, result)) {
            w.wakeNanos = System.nanoTime();
            clearInterest(pd, mode);
            SchedulerCore.current().unpark(w.handle);
        }
    }

    private static void setInterest(PollDesc pd, int mode, boolean on) {
        SelectionKey key = pd.key;
        if (key == null || !key.isValid()) {
            return;
        }
        int bit;
        if (mode == MODE_WRITE && pd.connecting) {
            bit = SelectionKey.OP_CONNECT;
        } else if (mode == MODE_READ && pd.listener) {
            bit = SelectionKey.OP_ACCEPT;
        } else {
            bit = mode == MODE_READ ? SelectionKey.OP_READ : SelectionKey.OP_WRITE;
        }
        try {
            int ops = key.interestOps();
            key.interestOps(on ? (ops | bit) : (ops & ~bit));
        } catch (CancelledKeyException ignored) {
            // Raced with close; the waiter will observe closing via checkErr
            // on its next wait, and the current wait's waker paths are
            // CAS-guarded.
        }
        Selector sel = selector;
        if (sel != null) {
            sel.wakeup();
        }
    }

    private static void clearInterest(PollDesc pd, int mode) {
        setInterest(pd, mode, false);
    }

    // ------------------------------------------------------------------
    // Deadlines (Phase-2 TimerHeap).
    // ------------------------------------------------------------------

    private static void armDeadline(PollDesc pd, int mode, long d) {
        if (d > 0) {
            long now = System.nanoTime();
            long abs = now + d;
            if (abs <= 0) {
                abs = Long.MAX_VALUE; // overflow guard (gc parity)
            }
            setDeadlineState(pd, mode, abs);
            SchedulerCore sched = SchedulerCore.current();
            // The callback validates its own id (stale fired timers are
            // ignored) — see onDeadline. The id hand-off is an AtomicLong
            // (P2-2): the old plain-array box had no happens-before edge
            // between the carrier's write (after addCallback returns) and
            // the timer thread's read. -1 = unpublished: a callback that
            // (formally) fires before the hand-off is dropped instead of
            // CAS-claiming -1 and delivering a spurious TIMEOUT (timer
            // ids start at 1, so -1 never collides).
            final AtomicLong idBox = new AtomicLong(-1);
            long id = sched.timerHeap().addCallback(abs,
                    () -> {
                        long myId = idBox.get();
                        if (myId >= 0) {
                            onDeadline(pd, mode, myId);
                        }
                    });
            idBox.set(id);
            setTimerId(pd, mode, id);
        } else {
            // d == 0: disarm. d < 0: expire immediately (gc: unblock
            // pending IO when the new deadline is in the past).
            cancelDeadline(pd, mode);
            if (d < 0) {
                setDeadlineState(pd, mode, -1);
                wakeWith(pd, mode, POLL_ERR_TIMEOUT);
            } else {
                setDeadlineState(pd, mode, 0);
            }
        }
    }

    private static void onDeadline(PollDesc pd, int mode, long myId) {
        // Stale-timer guard: only the currently armed timer may wake.
        // The CAS makes check-and-clear atomic: a callback that fired
        // just as pollSetDeadline re-armed either wins (its deadline
        // genuinely expired first) or loses (stale, ignored) — it can
        // never wipe the new timer's id.
        AtomicLong idRef = mode == MODE_READ ? pd.readTimerId : pd.writeTimerId;
        if (!idRef.compareAndSet(myId, -1)) {
            return;
        }
        // The deadline state was already expired when armed; the CAS
        // in wakeWith decides whether a waiter is still parked to notify.
        wakeWith(pd, mode, POLL_ERR_TIMEOUT);
    }

    private static void setDeadlineState(PollDesc pd, int mode, long abs) {
        if (mode == MODE_READ) {
            pd.readDeadline = abs;
        } else {
            pd.writeDeadline = abs;
        }
    }

    private static void setTimerId(PollDesc pd, int mode, long id) {
        AtomicLong idRef = mode == MODE_READ ? pd.readTimerId : pd.writeTimerId;
        long prev = idRef.getAndSet(id);
        if (prev >= 0 && prev != id) {
            SchedulerCore.current().timerHeap().cancel(prev);
        }
    }

    private static void cancelDeadline(PollDesc pd, int mode) {
        setTimerId(pd, mode, -1);
    }

    // ------------------------------------------------------------------
    // The poller thread.
    // ------------------------------------------------------------------

    private static void pollLoop(Selector sel) {
        for (;;) {
            try {
                drainRegistrations();
                int n = sel.select();
                drainRegistrations();
                if (n > 0) {
                    Set<SelectionKey> keys = sel.selectedKeys();
                    for (Iterator<SelectionKey> it = keys.iterator(); it.hasNext();) {
                        SelectionKey key = it.next();
                        it.remove();
                        try {
                            handleKey(key);
                        } catch (Throwable t) {
                            // One bad key must not kill the poller: record
                            // for tests, cancel the key, keep going.
                            lastPollerError = t;
                            try {
                                key.cancel();
                            } catch (Throwable ignored) {
                                // Best effort.
                            }
                        }
                    }
                }
            } catch (ClosedSelectorException e) {
                return; // resetForRun: shut down.
            } catch (Throwable t) {
                // Never let the poller thread die: every parked waiter
                // would hang forever. Record and continue (no stderr in
                // library code — the phase-3 debug-leftover gate).
                lastPollerError = t;
            }
        }
    }

    private static void drainRegistrations() {
        FutureTask<SelectionKey> task;
        while ((task = REG_Q.poll()) != null) {
            task.run();
        }
    }

    private static void handleKey(SelectionKey key) {
        Object att = key.attachment();
        if (!(att instanceof PollDesc pd)) {
            key.cancel();
            return;
        }
        if (pd.closed || !DESCS.containsKey(pd.id)) {
            key.cancel();
            return;
        }
        int ready;
        try {
            ready = key.readyOps();
        } catch (CancelledKeyException e) {
            return;
        }
        if ((ready & SelectionKey.OP_CONNECT) != 0) {
            finishConnect(pd);
        }
        int ioBits = ready
                & (SelectionKey.OP_READ | SelectionKey.OP_WRITE | SelectionKey.OP_ACCEPT);
        if (ioBits == 0) {
            // Spurious selection (no IO bits): nothing to wake for.
            // Real errors surface through the channel operations
            // themselves (read/write return -errno); never synthesize
            // NOT_POLLABLE here — that would be a false positive.
            return;
        }
        if ((ready & (SelectionKey.OP_READ | SelectionKey.OP_ACCEPT)) != 0) {
            wakeWith(pd, MODE_READ, POLL_NO_ERROR);
        }
        if ((ready & SelectionKey.OP_WRITE) != 0) {
            // Stale write-readiness on a connecting socket: OP_CONNECT
            // above already handled completion.
            if (!pd.connecting) {
                wakeWith(pd, MODE_WRITE, POLL_NO_ERROR);
            }
        }
    }

    private static void finishConnect(PollDesc pd) {
        if (!(pd.ch instanceof SocketChannel sc)) {
            return;
        }
        try {
            if (sc.finishConnect()) {
                pd.connecting = false;
                GoNet.onConnectComplete(pd.sockCookie, 0);
                clearInterestConnect(pd);
                wakeWith(pd, MODE_WRITE, POLL_NO_ERROR);
            }
            // finishConnect() == false with OP_CONNECT ready should not
            // happen; leaving interest armed is the safe fallback.
        } catch (IOException e) {
            pd.connecting = false;
            int errno = GoNet.sockErrno(e);
            GoNet.onConnectComplete(pd.sockCookie, errno);
            clearInterestConnect(pd);
            // gc parity: the waiter wakes without error; the failure
            // surfaces via getsockopt(SO_ERROR) on the next operation.
            wakeWith(pd, MODE_WRITE, POLL_NO_ERROR);
        }
    }

    private static void clearInterestConnect(PollDesc pd) {
        SelectionKey key = pd.key;
        if (key != null && key.isValid()) {
            try {
                key.interestOps(key.interestOps() & ~SelectionKey.OP_CONNECT);
            } catch (CancelledKeyException ignored) {
                // Raced with close.
            }
        }
    }

    // ------------------------------------------------------------------
    // Test hooks.
    // ------------------------------------------------------------------

    /** Test hook: the pollDesc for a ctx (to poke deadlines/state). */
    static PollDesc descForTest(long ctx) {
        return DESCS.get(ctx);
    }

    /** Test hook: the currently armed read timer id (-1 = none). */
    static long readTimerIdForTest(long ctx) {
        PollDesc pd = DESCS.get(ctx);
        return pd == null ? -1 : pd.readTimerId.get();
    }

    /** Test hook: the currently armed write timer id (-1 = none). */
    static long writeTimerIdForTest(long ctx) {
        PollDesc pd = DESCS.get(ctx);
        return pd == null ? -1 : pd.writeTimerId.get();
    }

    /**
     * Test hook: fires a deadline callback exactly as the timer thread
     * would — simulates a stale timer whose lazy {@code TimerHeap.cancel}
     * lost the race (P1-1).
     */
    static void fireDeadlineForTest(long ctx, int mode, long id) {
        PollDesc pd = DESCS.get(ctx);
        if (pd != null) {
            onDeadline(pd, mode, id);
        }
    }

    /**
     * Test hook: the CAS-win nanotime of the last completed wait on
     * {@code ctx} (0 if none). Measures the poller-side wakeup path
     * (epoll event → unpark decision) without waiter-side
     * carrier-scheduling noise.
     */
    public static long lastWakeNanosForTest(long ctx) {
        PollDesc pd = DESCS.get(ctx);
        Waiter w = pd == null ? null : pd.lastDone;
        return w == null ? 0 : w.wakeNanos;
    }
}
