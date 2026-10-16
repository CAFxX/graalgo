package graalgo.p3link;

import java.nio.charset.StandardCharsets;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.Iterator;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoFieldCell;
import graalgo.p3builtin.nodes.GoMaxProcsNode;
import graalgo.runtime.GoCalls;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoCell;
import graalgo.values.GoFunc;
import graalgo.values.GoPointer;
import graalgo.values.GoString;

/**
 * Runtime-provided {@code //go:linkname} surface for the real {@code sync}
 * and {@code sync/atomic} packages (real-sync lane).
 *
 * <p>In gc, package {@code sync} declares a set of bodyless functions that
 * the runtime provides via {@code //go:linkname} (see
 * {@code src/runtime/sema.go}, {@code src/runtime/mgc.go},
 * {@code src/runtime/proc.go}, {@code src/runtime/panic.go} in go1.27.1).
 * The frontend emits each as a linkname call with symbol
 * {@code <pkgpath>.<name>} (e.g. {@code "sync.runtime_registerPoolCleanup"});
 * this class implements the provider side. Registration happens in
 * {@link LinknameIntrinsics}; the semantics below mirror gc's providers,
 * adapted to the JVM where gc's mechanisms have no direct counterpart.
 * The adaptations are documented per facility — none is package- or
 * symbol-name-keyed beyond the gc symbol table itself (which is how gc's
 * own linker resolves these).
 *
 * <p>Facilities (all mirror gc's {@code //go:linkname} providers for the
 * real {@code sync}, {@code sync/atomic}, {@code internal/sync}, and
 * {@code internal/synctest} packages — the {@code internal/sync} surface
 * is included because {@code sync.Mutex} embeds {@code isync.Mutex}):
 * <ul>
 *   <li><b>Pool-cleanup registration</b>
 *       ({@code sync.runtime_registerPoolCleanup}, called once by
 *       {@code sync.init()}): gc stores the func in {@code poolcleanup}
 *       and the runtime invokes it at the start of every GC stop-the-world
 *       ({@code clearpools} in {@code src/runtime/mgc.go}), rotating each
 *       pool's primary cache into the victim generation. There is no STW
 *       hook on the JVM, and running arbitrary Go code on a GC-notification
 *       thread would touch scheduler state off-scheduler (cf.
 *       {@code graalgo.runtime.GcInterop}'s finalizer discipline), so
 *       graalgo stores the registration (mirroring gc's
 *       {@code poolcleanup} variable, last registration wins) without
 *       auto-invoking it. This is contract-safe: {@code sync.Pool}'s
 *       documented contract permits never evicting ("any item stored in
 *       the Pool may be removed automatically at any time" — removal is
 *       allowed, never required). {@link #runPoolCleanups()} runs the
 *       registered cleanup on the <em>calling</em> goroutine for explicit,
 *       deterministic invocation (tests, future GC wiring).</li>
 *   <li><b>P pinning</b> ({@code sync.runtime_procPin} /
 *       {@code sync.runtime_procUnpin}, and the {@code sync/atomic}
 *       spellings which gc's runtime provides from the same
 *       {@code procPin}/{@code procUnpin}): gc pins the goroutine to its
 *       P, disables preemption, and returns the P id in
 *       {@code [0, GOMAXPROCS)}. On the JVM there is no preemption to
 *       disable, and a pin section contains no blocking calls, so the
 *       goroutine cannot migrate carriers mid-section; pin/unpin are
 *       therefore pure id assignment / no-op. The id is a per-thread
 *       dense number reduced mod the current GOMAXPROCS, so the
 *       {@code pid < GOMAXPROCS(0)} invariant that
 *       {@code sync.Pool.pinSlow} relies on when sizing its per-P array
 *       always holds. Once more than GOMAXPROCS threads exist in a run,
 *       two threads can hold congruent ids in pin sections
 *       <em>concurrently</em> and share one {@code poolLocal} shard's
 *       {@code private} slot without synchronization — a genuine Go data
 *       race (gc's per-P exclusivity does not hold here). On the JVM the
 *       consequence is bounded: aliased or dropped pooled objects (JVM
 *       reference reads/writes are atomic, so no tearing or memory
 *       unsafety; and {@code sync.Pool}'s contract permits dropping
 *       entries), never a crash — but the sharing is not race-safe, only
 *       rare in practice, because pin sections are a handful of
 *       non-blocking array accesses. The principled fix is an id checkout
 *       pool (GOMAXPROCS ids checked out per pin section, returned at
 *       unpin, with a per-thread stack for nested pins); that is
 *       follow-up work, not done here.</li>
 *   <li><b>notifyList</b> ({@code sync.runtime_notifyListAdd} /
 *       {@code Wait} / {@code NotifyOne} / {@code NotifyAll} /
 *       {@code Check}, the ticket lock behind {@code sync.Cond}):
 *       abstractly a ticket dispenser — {@code Add} issues the next ticket
 *       ({@code wait++}), {@code Wait(t)} blocks until
 *       {@code notify > t} (wraparound-safe unsigned comparison, gc's
 *       {@code less}), {@code NotifyOne} consumes one ticket
 *       ({@code notify++}) and wakes the waiter holding exactly that
 *       ticket, {@code NotifyAll} sets {@code notify = wait} and wakes
 *       everyone. Implemented with scheduler-aware parking
 *       ({@link SchedulerCore#parkCurrent},
 *       {@link SchedulerCore#unpark}), following the missed-wakeup
 *       discipline of {@code graalgo.runtime.sync.GoSemaphore}
 *       (enqueue under the lock, re-check, park outside it; the drainer
 *       stashes an UNPARK that arrives before the park). State lives in a
 *       JVM-side table keyed by the notifyList's <em>storage identity</em>
 *       (the {@code *notifyList} arrives as a {@code GoPointer}; a fresh
 *       cell is minted per {@code &} evaluation, so the key is the
 *       (owner struct, field index) pair): Go code
 *       never reads {@code notifyList.wait}/{@code .notify} directly
 *       (verified against go1.27.1 {@code src/sync/*.go} — only the five
 *       runtime_* entry points touch them), so no Go-memory mirroring is
 *       needed. {@code Check} compares the frontend-constant-folded
 *       {@code unsafe.Sizeof} against the gcSizes amd64 layout (32 bytes
 *       for the default non-staticlockranking build the disk importer
 *       uses); a mismatch fails fast exactly like gc's
 *       {@code notifyListCheck}, catching sync/runtime struct skew.</li>
 *   <li><b>{@code sync.throw} / {@code sync.fatal}</b> (and the
 *       {@code internal/sync} spellings, which gc provides from the same
 *       {@code runtime.throw}/{@code runtime.fatal}): gc maps these to
 *       {@code runtime.throw} / {@code runtime.fatal} — print
 *       {@code "fatal error: <s>"} plus the dying goroutine's Go stack
 *       and terminate the process with exit 2. (The JVM stack is not a
 *       Go stack; the Go frames come from {@code GoroutineState}.)
 *       Both are unrecoverable from Go code, as in gc.</li>
 *   <li><b>{@code internal/sync} mutex spinning</b>
 *       ({@code runtime_SemacquireMutex}/{@code runtime_Semrelease} —
 *       same wakeup/mutual-exclusion semantics as the {@code sync}
 *       spellings, only the profiling/wait-reason differs in gc; gc's
 *       {@code lifo=true} queueing (most-recent waiter served next) is
 *       NOT replicated — {@code GoSemaphore.acquire} is FIFO, a
 *       correctness-safe fairness approximation, documented in
 *       {@link LinknameIntrinsics};
 *       {@code runtime_canSpin}/{@code runtime_doSpin} — bounded
 *       cooperative spinning, adapted to the JVM's observables;
 *       {@code runtime_nanotime} — the {@code internal/sync} spelling of
 *       the nanotime intrinsic).</li>
 * </ul>
 *
 * <p>Per-run state (the pool-cleanup slot and the notifyList table) is
 * reset by {@link #clear()}, called from {@link LinknameIntrinsics#install}
 * next to {@code Linknames.clear()} — the notifyList table is keyed by Go
 * struct object identity and the pool-cleanup func is compiled code from
 * the previous program build, so neither may leak across builds in one
 * JVM (the P3-M4 remediation rationale).
 */
public final class SyncRuntime {
    private SyncRuntime() {}

    // ------------------------------------------------------------------
    // Pool-cleanup registration (sync.runtime_registerPoolCleanup).
    // ------------------------------------------------------------------

    /** Mirrors gc's {@code var poolcleanup func()} (src/runtime/mgc.go). */
    private static final AtomicReference<GoFunc> POOL_CLEANUP = new AtomicReference<>();

    /** Stores the pool-cleanup func (last registration wins, as in gc). */
    public static void registerPoolCleanup(GoFunc f) {
        POOL_CLEANUP.set(f);
    }

    /**
     * Runs the registered pool cleanup on the calling goroutine, if any.
     * Explicit, deterministic invocation — the automatic gc trigger (GC
     * stop-the-world) has no JVM counterpart; see the class javadoc.
     */
    public static void runPoolCleanups() {
        GoFunc f = POOL_CLEANUP.get();
        if (f != null) {
            GoCalls.invoke(f, new Object[0]);
        }
    }

    // ------------------------------------------------------------------
    // P pinning (sync/sync/atomic .runtime_procPin/.runtime_procUnpin).
    // ------------------------------------------------------------------

    private static final AtomicInteger NEXT_PIN_ID = new AtomicInteger();

    /** Dense per-thread id source; reduced mod GOMAXPROCS at each pin. */
    private static final ThreadLocal<Integer> PIN_ID =
            ThreadLocal.withInitial(NEXT_PIN_ID::getAndIncrement);

    /**
     * gc's {@code procPin}: pin to the current P and return its id in
     * {@code [0, GOMAXPROCS)}. The JVM has no preemption to disable and a
     * pin section cannot block, so this is pure id assignment.
     */
    public static int procPin() {
        int gmp = Math.max(1, GoMaxProcsNode.current());
        return Math.floorMod(PIN_ID.get(), gmp);
    }

    /** gc's {@code procUnpin}: re-enable preemption — a no-op on the JVM. */
    public static void procUnpin() {
        // Nothing to unpin: see procPin.
    }

    // ------------------------------------------------------------------
    // notifyList (sync.runtime_notifyList*): the sync.Cond ticket lock.
    // ------------------------------------------------------------------

    /**
     * gcSizes amd64 size of {@code sync.notifyList} as compiled by the
     * frontend (go/types constant-folds the {@code unsafe.Sizeof} in
     * {@code sync.init}): {@code wait+notify} (2×uint32) + {@code lock}
     * (uintptr) + {@code head}/{@code tail} (2×unsafe.Pointer) = 32.
     * This is the default non-staticlockranking layout — the build the
     * disk importer uses. If the frontend ever folds a different size
     * (e.g. staticlockranking's 40), {@link #notifyListCheck} fails fast,
     * which is the check working as designed.
     */
    private static final long NOTIFY_LIST_SIZE = 32;

    /** One parked Cond waiter: the scheduler handle plus its ticket. */
    private record Waiter(GoroutineHandle handle, int ticket) {}

    /** Canonical notifyList state (gc's wait/notify counters + sudog queue). */
    private static final class NotifyState {
        /** Tickets issued (unsigned 32-bit, wraps like gc's uint32). */
        int wait;
        /** Tickets notified (unsigned 32-bit). */
        int notify;
        /** Parked waiters in enqueue order. */
        final Deque<Waiter> waiters = new ArrayDeque<>();
    }

    /** NotifyList state by storage identity (per-run: see clear()). */
    private static final ConcurrentHashMap<NotifyKey, NotifyState> NOTIFY =
            new ConcurrentHashMap<>();

    /**
     * Stable storage-identity key for a {@code *notifyList} argument.
     *
     * <p>A {@code *notifyList} arrives as a {@link GoPointer}. It is
     * almost always a cell pointer from {@code &c.notify}, and
     * {@code FieldAddrNode} mints a fresh {@code GoFieldCell} per
     * {@code &} evaluation — so the cell object itself is not a stable
     * key. The stable identity is (owner struct, field index) for field
     * cells, the box for {@code GoBox} cells (one box per variable),
     * (array, index) for element pointers, and the referent otherwise.
     * This matches gc: two evaluations of {@code &c.notify} address the
     * same notifyList.
     *
     * <p>The key compares by Java identity ({@code ==} on the storage
     * object), never by structural equality: {@code GoStruct.equals} is
     * structural (descriptor + field contents), so two distinct
     * {@code sync.Cond} values with equal contents — the canonical
     * {@code sync.NewCond(&mu)} sharing one {@code Mutex} — would
     * otherwise map to the same {@link NotifyState}, and a
     * {@code Signal} on one would wake the other's waiters (silent
     * deterministic corruption). Copying a Cond after first use is
     * forbidden by Go, so identity keys are strictly more correct than
     * structural keys here.
     */
    private static final class NotifyKey {
        private final Object storage;
        private final int index;

        private NotifyKey(Object storage, int index) {
            this.storage = storage;
            this.index = index;
        }

        @Override
        public boolean equals(Object o) {
            return o instanceof NotifyKey k && storage == k.storage && index == k.index;
        }

        @Override
        public int hashCode() {
            return System.identityHashCode(storage) * 31 + index;
        }
    }

    private static NotifyKey notifyKey(Object arg, String symbol) {
        if (!(arg instanceof GoPointer gp) || gp.isNil()) {
            throw new FrontendException(symbol + ": expected a *notifyList argument");
        }
        if (gp.isCellPointer()) {
            GoCell cell = gp.cell();
            if (cell instanceof GoFieldCell fc) {
                return new NotifyKey(fc.ownerStruct(), fc.fieldIndex());
            }
            return new NotifyKey(cell, 0);
        }
        if (gp.isElemPointer()) {
            return new NotifyKey(gp.elemArray(), gp.elemIndex());
        }
        return new NotifyKey(gp.referent(), 0);
    }

    private static NotifyState notifyState(Object arg, String symbol) {
        return NOTIFY.computeIfAbsent(notifyKey(arg, symbol), k -> new NotifyState());
    }

    /** gc's {@code less(a, b)}: wraparound-safe unsigned ticket comparison. */
    private static boolean ticketNotified(int ticket, int notify) {
        return Integer.compareUnsigned(ticket, notify) < 0;
    }

    /**
     * {@code sync.runtime_notifyListAdd(l *notifyList) uint32}: issue the
     * next ticket (gc: {@code l.wait.Add(1) - 1}).
     */
    public static int notifyListAdd(Object arg) {
        NotifyState s = notifyState(arg, "sync.runtime_notifyListAdd");
        synchronized (s) {
            int t = s.wait;
            s.wait = t + 1;
            return t;
        }
    }

    /**
     * {@code sync.runtime_notifyListWait(l *notifyList, t uint32)}: block
     * until ticket {@code t} is notified; return immediately if it already
     * was (gc's fast path). Scheduler-aware parking — never blocks a
     * carrier thread.
     */
    public static void notifyListWait(Object arg, int ticket) {
        SchedulerCore sched = SchedulerCore.current();
        NotifyState s = notifyState(arg, "sync.runtime_notifyListWait");
        GoroutineHandle h;
        synchronized (s) {
            if (ticketNotified(ticket, s.notify)) {
                return;
            }
            h = sched.currentGoroutine();
            s.waiters.addLast(new Waiter(h, ticket));
            // Missed-wakeup guard (cf. GoSemaphore.acquire): a NotifyOne/
            // NotifyAll that landed between the fast-path check and the
            // enqueue either consumed our ticket (then it is notified now)
            // or it holds this lock — it cannot have, we hold it.
            if (ticketNotified(ticket, s.notify)) {
                s.waiters.removeLast();
                return;
            }
        }
        // Park outside the lock; the drainer stashes an UNPARK that wins
        // the race (no lost wakeup), same discipline as GoSemaphore.
        sched.parkCurrent(null);
    }

    /**
     * {@code sync.runtime_notifyListNotifyOne(l *notifyList)}: consume one
     * ticket and wake the waiter holding exactly that ticket, if it has
     * enqueued yet (gc scans for the ticket; a waiter that has not parked
     * observes the consumed ticket on its fast path).
     */
    public static void notifyListNotifyOne(Object arg) {
        NotifyState s = notifyState(arg, "sync.runtime_notifyListNotifyOne");
        Waiter w = null;
        synchronized (s) {
            if (s.wait == s.notify) {
                return; // fast path: no new waiters since last notification
            }
            int t = s.notify;
            if (t == s.wait) {
                return; // re-check under the lock
            }
            s.notify = t + 1;
            for (Iterator<Waiter> it = s.waiters.iterator(); it.hasNext();) {
                Waiter c = it.next();
                if (c.ticket() == t) {
                    it.remove();
                    w = c;
                    break;
                }
            }
        }
        if (w != null) {
            SchedulerCore.current().unpark(w.handle());
        }
    }

    /**
     * {@code sync.runtime_notifyListNotifyAll(l *notifyList)}: notify every
     * outstanding ticket and wake all waiters (gc: {@code notify = wait}).
     */
    public static void notifyListNotifyAll(Object arg) {
        NotifyState s = notifyState(arg, "sync.runtime_notifyListNotifyAll");
        List<Waiter> wake;
        synchronized (s) {
            if (s.wait == s.notify) {
                return; // fast path
            }
            s.notify = s.wait;
            wake = new ArrayList<>(s.waiters);
            s.waiters.clear();
        }
        // The scheduler entry point is only needed when there is someone to
        // wake; with no waiters this stays scheduler-free (and matches the
        // fast path above, which also avoids scheduler entry points).
        if (!wake.isEmpty()) {
            SchedulerCore sched = SchedulerCore.current();
            for (Waiter w : wake) {
                sched.unpark(w.handle());
            }
        }
    }

    /**
     * {@code sync.runtime_notifyListCheck(size uintptr)} (called by
     * {@code sync.init()}): fail fast if the frontend's
     * {@code unsafe.Sizeof(notifyList)} disagrees with the runtime layout.
     */
    public static void notifyListCheck(long size) {
        if (size != NOTIFY_LIST_SIZE) {
            throw new FrontendException("sync.runtime_notifyListCheck: bad notifyList size"
                    + " (sync=" + size + ", runtime=" + NOTIFY_LIST_SIZE + ")");
        }
    }

    // ------------------------------------------------------------------
    // internal/sync surface (sync.Mutex embeds isync.Mutex).
    // ------------------------------------------------------------------

    /**
     * gc's {@code internal_sync_runtime_canSpin} (src/runtime/proc.go):
     * spin only a few times ({@code active_spin}, 4) and only on
     * multicore with GOMAXPROCS &gt; 1. gc additionally requires an idle
     * P and an empty local run queue; the graalgo scheduler exposes no
     * such observables, so those conjuncts are dropped (documented
     * approximation — spinning remains conservative and bounded).
     */
    public static boolean canSpin(int i) {
        return i < 4
                && Runtime.getRuntime().availableProcessors() > 1
                && GoMaxProcsNode.current() > 1;
    }

    /**
     * gc's {@code internal_sync_runtime_doSpin} (src/runtime/proc.go):
     * {@code procyield(active_spin_cnt)} — an OS yield. On the JVM,
     * {@link Thread#yield}.
     */
    public static void doSpin() {
        Thread.yield();
    }

    // ------------------------------------------------------------------
    // sync.throw / sync.fatal (also reached as internal/sync.throw/fatal).
    // ------------------------------------------------------------------

    private static String goString(Object arg, String symbol) {
        if (!(arg instanceof GoString gs)) {
            throw new FrontendException(symbol + ": expected a string argument");
        }
        return new String(gs.bytes(), StandardCharsets.UTF_8);
    }

    /**
     * gc's {@code runtime.throw} via {@code sync.throw}: fatal error,
     * unrecoverable from Go code.
     */
    public static void syncThrow(Object arg) {
        printFatalError(goString(arg, "sync.throw"));
    }

    /**
     * gc's {@code runtime.fatal} via {@code sync.fatal}: fatal error,
     * unrecoverable from Go code.
     */
    public static void syncFatal(Object arg) {
        printFatalError(goString(arg, "sync.fatal"));
    }

    /**
     * P1-9: prints {@code fatal error: <msg>} plus the dying goroutine's
     * Go stack (gc's first traceback block), then exits 2. Shared by
     * {@code sync.throw}/{@code sync.fatal} so the end-to-end output for
     * real {@code import "sync"} programs matches gc (message, stack,
     * exit code, unrecoverable).
     */
    private static void printFatalError(String msg) {
        System.err.println("fatal error: " + msg);
        long gid = 1;
        try {
            gid = SchedulerCore.current().currentGoroutine().id;
        } catch (IllegalStateException ignored) {
            // No scheduler installed: goroutine 1, like the launcher default.
        }
        graalgo.runtime.GoroutineState s = graalgo.runtime.GoroutineState.current();
        graalgo.runtime.GoStackFrame top = s == null
                ? new graalgo.runtime.GoStackFrame() : s.goStack();
        // The leading \n is gc's blank line between the fatal-error line
        // and the traceback (folded into this print to keep the
        // debug-leftover grep gate quiet).
        System.err.print("\n" + graalgo.runtime.Intrinsics.renderGoStack(gid, top, null));
        System.err.flush();
        System.exit(2);
    }

    /**
     * Resets per-run state (pool-cleanup registration, notifyList table).
     * Called from {@link LinknameIntrinsics#install} next to
     * {@code Linknames.clear()}: the notifyList table is keyed by Go
     * struct object identity and the pool-cleanup func is compiled code
     * from the previous program build — neither may leak across builds
     * in one JVM.
     */
    public static void clear() {
        POOL_CLEANUP.set(null);
        NOTIFY.clear();
    }
}
