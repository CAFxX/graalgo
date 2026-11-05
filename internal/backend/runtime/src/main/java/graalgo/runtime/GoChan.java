package graalgo.runtime;

import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.Iterator;
import java.util.List;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;
import graalgo.jfr.ChannelOp;
import graalgo.jfr.JfrEvents;
import graalgo.jfr.ParkReason;

/**
 * Go channel implementation (P2-M2, WS B; GG-0017 chan slice).
 *
 * <p>Adapted from Go's {@code runtime/chan.go} to scheduler-aware park:
 * every blocking operation parks the goroutine via
 * {@link SchedulerCore#parkCurrent} (never blocks a carrier thread) and
 * every handoff wakes via {@link SchedulerCore#unpark} through the
 * drainer's WAKE lane.
 *
 * <p>Concurrency protocol: all channel state (buffer, closed flag, waiter
 * queues) is guarded by the channel monitor. A waiter enqueues itself
 * under the monitor and only then parks (outside the monitor — the waker
 * needs it). Lost-wakeup freedom comes from the P2-M1 drainer protocol:
 * the drainer matches PARKED/UNPARK tokens by handle tolerating both
 * orders, so a waker that dequeues + unparks before the waiter parks
 * cannot lose the wakeup. The waiter re-checks its outcome under the
 * monitor after resume.
 *
 * <p>Waiter queues are FIFO (Go fairness): {@code sendWaiters} and
 * {@code recvWaiters} are separate, matching Go's sendq/recvq. A direct
 * sender→receiver handoff bypasses the buffer.
 *
 * <p>State-machine core: monitor-held {@link #trySend} /
 * {@link #tryRecv} are the single non-blocking primitives; {@link #send},
 * {@link #recv} and {@link SelectOp} all build on them, so the three
 * agree exactly. There is no check-then-act split: trySend/tryRecv
 * either complete or report "would block" atomically under one monitor
 * hold (a select waiter claimed by another channel between the readiness
 * peek and the handoff just makes trySend report WOULD_BLOCK; a waiter
 * whose selector transiently self-claimed it makes trySend report RETRY
 * instead of dropping the waiter).
 *
 * <p>Subclassing (P3-M2a): the class is intentionally non-final for the
 * single dedicated {@link TimerChan} flavor. The masking contract is:
 * {@code buffered()} is called only from {@code ChanOps.len} and
 * {@code capacity()} only from {@code ChanOps.cap}; ALL internal logic
 * (send/recv/select/waiters/{@code trySend}/{@code tryRecv}) uses the
 * buffer fields directly. A subclass may therefore mask the observation
 * probes without changing delivery behavior.
 */
public class GoChan implements ChanOps.Chan {
    private static final java.util.concurrent.atomic.AtomicLong ID_GEN =
            new java.util.concurrent.atomic.AtomicLong(1);

    /**
     * Stable channel identity for JFR ({@code graalgo.ChannelBlock}).
     * Diagnostic only; never observed by the Go program.
     */
    final long channelId = ID_GEN.getAndIncrement();

    /** Circular buffer; length == capacity (0 for unbuffered). */
    private final Object[] buf;
    private int head;
    private int tail;
    private int count;
    private boolean closed;
    /** The element zero value, for recv-on-closed-drained. */
    private final Object zeroValue;

    private final Deque<ChanWaiter> sendWaiters = new ArrayDeque<>();
    private final Deque<ChanWaiter> recvWaiters = new ArrayDeque<>();

    /**
     * @param capacity buffer capacity (0 = unbuffered)
     * @param zeroValue element zero value observed by recv on a closed,
     *                  drained channel
     */
    public GoChan(int capacity, Object zeroValue) {
        if (capacity < 0) {
            throw new IllegalArgumentException("negative channel capacity");
        }
        this.buf = new Object[capacity];
        this.zeroValue = zeroValue;
    }

    /** Buffer capacity. */
    public synchronized int capacity() {
        return buf.length;
    }

    /** Buffered element count (Go's {@code len(ch)}: waiters excluded). */
    public synchronized int buffered() {
        return count;
    }

    public synchronized boolean isClosed() {
        return closed;
    }

    /**
     * Number of parked send waiters. Test-only deterministic probe
     * (replaces sleep-gated "must be parked" assertions).
     */
    public synchronized int sendWaitersCount() {
        return sendWaiters.size();
    }

    /**
     * Number of parked recv waiters. Test-only deterministic probe.
     */
    public synchronized int recvWaitersCount() {
        return recvWaiters.size();
    }

    // ================= waiter records =================

    /** A goroutine parked on this channel (plain or select case). */
    abstract static class ChanWaiter {
        final GoroutineHandle handle;

        ChanWaiter(GoroutineHandle handle) {
            this.handle = handle;
        }

        /**
         * Called under the channel monitor when this waiter is dequeued
         * for a handoff. Returns true if it accepts (a select waiter may
         * already be claimed by another channel — then it is dropped).
         */
        abstract boolean accept();
    }

    /** Plain blocking send: carries the value; may be told to panic. */
    static final class SendWaiter extends ChanWaiter {
        final Object value;
        /** Set by close(): this send must panic on resume. */
        boolean panicOnWake;

        SendWaiter(GoroutineHandle handle, Object value) {
            super(handle);
            this.value = value;
        }

        @Override
        boolean accept() {
            return true;
        }
    }

    /** Plain blocking recv: receives the value + ok on wakeup. */
    static final class RecvWaiter extends ChanWaiter {
        Object value;
        boolean ok;

        RecvWaiter(GoroutineHandle handle) {
            super(handle);
        }

        @Override
        boolean accept() {
            return true;
        }
    }

    /** Result of a receive: value + ok (false = zero from closed). */
    public record RecvResult(Object value, boolean ok) {}

    // ================= monitor-held primitives =================
    // (Caller must hold the monitor.)

    /**
     * Non-mutating readiness for select polling: would {@link #trySend}
     * succeed? Purges claimed select waiters from the queue heads so the
     * answer reflects reality.
     */
    boolean canSend() {
        purgeClaimed(recvWaiters);
        return hasAcceptingWaiter(recvWaiters) || count < buf.length;
    }

    /**
     * Non-mutating readiness for select polling: would {@link #tryRecv}
     * succeed (non-null)?
     */
    boolean canRecv() {
        purgeClaimed(sendWaiters);
        return hasAcceptingWaiter(sendWaiters) || count > 0 || closed;
    }

    /**
     * Drops select waiters already claimed by a waker from a queue
     * head. A SELF_CLAIMed waiter is NOT dropped: its selector is
     * mid-sweep, no wakeup is in flight for it, and purging it strands
     * the selector (CI run 41943236460 lost wakeup).
     */
    private void purgeClaimed(Deque<ChanWaiter> q) {
        while (!q.isEmpty()) {
            ChanWaiter w = q.peekFirst();
            if (w instanceof SelectOp.SelectCaseWaiter sw && sw.isClaimedByWaker()) {
                q.pollFirst();
            } else {
                break;
            }
        }
    }

    /**
     * Would trySend/tryRecv actually accept a waiter from this queue
     * right now? A plain waiter, or a select waiter whose claim is
     * still free. Self-claimed waiters (selector mid-sweep) and
     * waker-claimed waiters (wake in flight) are not accepting. This
     * mirrors pollAccepting exactly, so Phase-1 readiness never
     * promises a completion trySend/tryRecv cannot keep.
     */
    private static boolean hasAcceptingWaiter(Deque<ChanWaiter> q) {
        for (ChanWaiter w : q) {
            if (w instanceof SelectOp.SelectCaseWaiter sw) {
                if (!sw.isClaimed()) {
                    return true;
                }
            } else {
                return true;
            }
        }
        return false;
    }

    /** Outcome of a non-blocking send attempt (monitor-held). */
    enum SendAttempt {
        /** Sent (direct handoff or buffered). */
        SENT,
        /** Would block: no receiver and no buffer space. */
        WOULD_BLOCK,
        /**
         * Blocked only by a self-claimed select waiter: another select
         * holds its transient self-claim on this channel's queue. The
         * caller must retry (outside the monitor / without holding a
         * self-claim) once that claim resolves — never drop the waiter
         * and never treat this as "would block" and park: the waiter
         * carries no wakeup for the blocked party, and parking on a
         * RETRY strands the caller (CI run 41943236460 lost wakeup).
         */
        RETRY
    }

    /** Outcome of a non-blocking receive attempt (monitor-held). */
    record RecvAttempt(RecvResult result, boolean retry) {}

    /**
     * Attempts a non-blocking send under the monitor. SENT if the send
     * completed (direct handoff or buffered); WOULD_BLOCK if it would
     * block; RETRY if the only waiter is a self-claimed select waiter
     * (see {@link SendAttempt#RETRY}). A buffered completion is taken
     * even past a self-claimed waiter: the waiter belongs to a select
     * completing elsewhere, and the value is genuinely stored. Never
     * parks.
     */
    SendAttempt trySend(Object v, SchedulerCore sched) {
        ChanWaiter r = pollAccepting(recvWaiters);
        if (r != null && r != RETRY_WAIT) {
            deliverToReceiver(r, v, true, sched);
            return SendAttempt.SENT;
        }
        if (count < buf.length) {
            buf[tail] = v;
            tail = (tail + 1) % buf.length;
            count++;
            return SendAttempt.SENT;
        }
        return r == RETRY_WAIT ? SendAttempt.RETRY : SendAttempt.WOULD_BLOCK;
    }

    /**
     * Attempts a non-blocking receive under the monitor. Returns the
     * result (or null result when it would block), plus a retry flag:
     * retry is true only when the buffer is empty, the channel is open,
     * and the only sender waiter is a self-claimed select waiter (see
     * {@link SendAttempt#RETRY} — same contract, receive side). A
     * buffered value, or the closed-channel zero, is returned even past
     * a self-claimed waiter: the waiter belongs to a select completing
     * elsewhere. Never parks.
     */
    RecvAttempt tryRecv(SchedulerCore sched) {
        ChanWaiter s = pollAccepting(sendWaiters);
        if (s != null && s != RETRY_WAIT) {
            return new RecvAttempt(new RecvResult(takeFromSender(s, sched), true), false);
        }
        if (count > 0) {
            Object v = buf[head];
            buf[head] = null;
            head = (head + 1) % buf.length;
            count--;
            return new RecvAttempt(new RecvResult(v, true), false);
        }
        if (closed) {
            return new RecvAttempt(new RecvResult(zeroValue, false), false);
        }
        return new RecvAttempt(null, s == RETRY_WAIT);
    }

    /**
     * Sentinel waiter returned by {@link #pollAccepting} when the queue
     * holds no accepting waiter but a self-claimed select waiter blocks
     * it. Never accepted, never delivered to: callers must retry (see
     * {@link SendAttempt#RETRY}). The sentinel's null handle is never
     * dereferenced — it is filtered out before any deliver/take.
     */
    private static final ChanWaiter RETRY_WAIT = new ChanWaiter(null) {
        @Override
        boolean accept() {
            throw new AssertionError("RETRY sentinel must never be accepted");
        }
    };

    /**
     * Dequeues the first accepting waiter, dropping select waiters
     * already claimed by another channel (their wakeup is in flight).
     * A self-claimed select waiter is SKIPPED (left enqueued) and, if
     * no accepting waiter is found behind it, reported via
     * {@link #RETRY_WAIT}: the caller must retry once the transient
     * self-claim resolves. Dropping it, or treating the queue as
     * empty and parking, strands its selector (CI run 41943236460
     * lost wakeup). Returns null if the queue is empty (or holds only
     * waker-claimed waiters, which are dropped).
     */
    private ChanWaiter pollAccepting(Deque<ChanWaiter> q) {
        boolean sawSelfClaimed = false;
        Iterator<ChanWaiter> it = q.iterator();
        while (it.hasNext()) {
            ChanWaiter w = it.next();
            if (w instanceof SelectOp.SelectCaseWaiter sw && sw.isSelfClaimed()) {
                sawSelfClaimed = true;
                continue;
            }
            it.remove();
            if (w.accept()) {
                return w;
            }
        }
        return sawSelfClaimed ? RETRY_WAIT : null;
    }

    /** Hands value v to a dequeued receiver waiter (plain or select). */
    private void deliverToReceiver(ChanWaiter r, Object v, boolean ok, SchedulerCore sched) {
        if (r instanceof RecvWaiter rw) {
            rw.value = v;
            rw.ok = ok;
            sched.unpark(rw.handle);
        } else {
            SelectOp.SelectCaseWaiter sw = (SelectOp.SelectCaseWaiter) r;
            sw.op.setResult(sw.caseIndex, v, ok);
            sched.unpark(sw.handle);
        }
    }

    /**
     * Takes the value from a dequeued sender waiter (plain or select).
     * For buffered channels the sender's value refills the freed buffer
     * slot (runtime/chan.go); unbuffered takes it directly.
     */
    private Object takeFromSender(ChanWaiter s, SchedulerCore sched) {
        Object sv;
        if (s instanceof SendWaiter sw) {
            sv = sw.value;
        } else {
            SelectOp.SelectCaseWaiter sel = (SelectOp.SelectCaseWaiter) s;
            sv = sel.sendValue;
            sel.op.setResult(sel.caseIndex, null, true);
        }
        Object v;
        if (count > 0) {
            v = buf[head];
            buf[head] = null; // P2-1: avoid retaining the consumed slot
            head = (head + 1) % buf.length;
            buf[tail] = sv;
            tail = (tail + 1) % buf.length;
            // count unchanged: one out, one in.
        } else {
            v = sv; // unbuffered direct handoff
        }
        sched.unpark(s.handle);
        return v;
    }

    // ================= public operations =================

    /**
     * Sends {@code v}, parking scheduler-aware if no receiver and no
     * buffer space. Panics (Go: "send on closed channel") if closed.
     */
    public void send(Object v) {
        SchedulerCore sched = SchedulerCore.current();
        SendWaiter w = null;
        boolean enqueued = false;
        // RETRY loop: a self-claimed select waiter blocks the queue
        // only transiently (its selector releases the self-claim as
        // soon as its sweep step finishes). Retry outside the monitor
        // so the selector can proceed; never enqueue-and-park on a
        // RETRY — the waiter carries no wakeup for us.
        while (!enqueued) {
            synchronized (this) {
                if (closed) {
                    Panic.throwPanic(GoStrings.of("send on closed channel"));
                }
                SendAttempt a = trySend(v, sched);
                if (a == SendAttempt.SENT) {
                    return;
                }
                if (a == SendAttempt.RETRY) {
                    // Fall through to the yield below.
                } else {
                    w = new SendWaiter(sched.currentGoroutine(), v);
                    sendWaiters.addLast(w);
                    enqueued = true;
                }
            }
            if (!enqueued) {
                Thread.yield();
            }
        }
        // Outside the monitor: scheduler-aware park (P2-D4: never blocks
        // the carrier). A waker that dequeued us before we park has its
        // UNPARK stashed by the drainer — no wakeup lost.
        // P2-M6: the send genuinely blocks — record it before parking.
        JfrEvents.channelBlock(w.handle, channelId, ChannelOp.SEND);
        sched.parkCurrent(null, ParkReason.CHANNEL);
        synchronized (this) {
            if (w.panicOnWake) {
                Panic.throwPanic(GoStrings.of("send on closed channel"));
            }
            // Otherwise a receiver took our value via direct handoff.
        }
    }

    /**
     * Receives, parking scheduler-aware if empty. Returns
     * {@code (zeroValue, false)} once closed and drained.
     */
    public RecvResult recv() {
        SchedulerCore sched = SchedulerCore.current();
        RecvWaiter w = null;
        boolean enqueued = false;
        // RETRY loop: same contract as send() — a self-claimed select
        // waiter blocks the queue only transiently; retry outside the
        // monitor, never enqueue-and-park on a RETRY.
        while (!enqueued) {
            synchronized (this) {
                RecvAttempt a = tryRecv(sched);
                if (!a.retry() && a.result() != null) {
                    return a.result();
                }
                if (a.retry()) {
                    // Fall through to the yield below.
                } else {
                    w = new RecvWaiter(sched.currentGoroutine());
                    recvWaiters.addLast(w);
                    enqueued = true;
                }
            }
            if (!enqueued) {
                Thread.yield();
            }
        }
        // P2-M6: the receive genuinely blocks — record it before parking.
        JfrEvents.channelBlock(w.handle, channelId, ChannelOp.RECV);
        sched.parkCurrent(null, ParkReason.CHANNEL);
        synchronized (this) {
            return new RecvResult(w.value, w.ok);
        }
    }

    /**
     * Closes the channel. Panics (Go: "close of closed channel") if
     * already closed. Receivers are woken with (zero, ok=false); senders
     * are woken to panic; select waiters are claimed or poisoned.
     */
    public void close() {
        SchedulerCore sched = SchedulerCore.current();
        // Self-claimed select waiters are DEFERRED, not dropped and not
        // cleared: their selector is mid-sweep, no wakeup is in flight
        // for them, and dropping them strands the selector (CI run
        // 41943236460 lost wakeup). The retry pass below claims each
        // deferred waiter as soon as its transient self-claim resolves
        // (or stops when the selector decides elsewhere and cleans up).
        // Retry runs OUTSIDE the monitor so the selector can proceed.
        List<ChanWaiter> deferred = new ArrayList<>();
        synchronized (this) {
            if (closed) {
                Panic.throwPanic(GoStrings.of("close of closed channel"));
            }
            closed = true;
            Iterator<ChanWaiter> rit = recvWaiters.iterator();
            while (rit.hasNext()) {
                ChanWaiter r = rit.next();
                if (r instanceof RecvWaiter rw) {
                    rw.value = zeroValue;
                    rw.ok = false;
                    sched.unpark(rw.handle);
                    rit.remove();
                } else if (r instanceof SelectOp.SelectCaseWaiter sw && sw.isSelfClaimed()) {
                    deferred.add(sw);
                } else if (r.accept()) {
                    // Select recv case: closed is always ready.
                    SelectOp.SelectCaseWaiter sw = (SelectOp.SelectCaseWaiter) r;
                    sw.op.setResult(sw.caseIndex, zeroValue, false);
                    sched.unpark(sw.handle);
                    rit.remove();
                } else {
                    // Already claimed by another case: wake in flight, drop.
                    rit.remove();
                }
            }
            Iterator<ChanWaiter> sit = sendWaiters.iterator();
            while (sit.hasNext()) {
                ChanWaiter s = sit.next();
                if (s instanceof SendWaiter sw) {
                    sw.panicOnWake = true;
                    sched.unpark(sw.handle);
                    sit.remove();
                } else if (s instanceof SelectOp.SelectCaseWaiter sw && sw.isSelfClaimed()) {
                    deferred.add(sw);
                } else if (s.accept()) {
                    // Select send case on a channel being closed: poison.
                    // The select panics on resume ("send on closed
                    // channel"); see SelectOp. A select that already
                    // completed via another case fails accept() and is
                    // simply dropped here.
                    SelectOp.SelectCaseWaiter sw = (SelectOp.SelectCaseWaiter) s;
                    sw.op.setPoison(sw.caseIndex);
                    sched.unpark(sw.handle);
                    sit.remove();
                } else {
                    // Already claimed by another case: wake in flight, drop.
                    sit.remove();
                }
            }
            // No clear(): every waiter was removed or deferred. A closed
            // channel admits no new waiters (send panics, recv and
            // select-recv complete immediately), so the queues now hold
            // only deferred entries, resolved by the retry pass.
        }
        for (ChanWaiter d : deferred) {
            SelectOp.SelectCaseWaiter sw = (SelectOp.SelectCaseWaiter) d;
            for (;;) {
                boolean retry = false;
                synchronized (this) {
                    Deque<ChanWaiter> q = sw.isSend ? sendWaiters : recvWaiters;
                    if (!q.contains(sw)) {
                        break; // Selector decided elsewhere and cleaned up.
                    }
                    if (sw.isClaimedByWaker()) {
                        q.remove(sw); // Decided elsewhere; drop.
                        break;
                    }
                    if (sw.isSelfClaimed()) {
                        retry = true;
                    } else if (sw.accept()) {
                        if (sw.isSend) {
                            sw.op.setPoison(sw.caseIndex);
                        } else {
                            sw.op.setResult(sw.caseIndex, zeroValue, false);
                        }
                        sched.unpark(sw.handle);
                        q.remove(sw);
                        break;
                    } else {
                        q.remove(sw); // Lost the race to a waker claim; decided.
                        break;
                    }
                }
                if (retry) {
                    Thread.yield();
                }
            }
        }
    }

    // ================= select support (package) =================

    /** Enqueues a select waiter on the send or recv queue. */
    void enqueueSelectWaiter(SelectOp.SelectCaseWaiter w, boolean isSend) {
        // Caller holds the monitor.
        (isSend ? sendWaiters : recvWaiters).addLast(w);
    }

    /**
     * Removes a select waiter entry (resumed goroutine cleaning up its
     * non-winning entries). Acquires the monitor.
     */
    void removeSelectWaiter(SelectOp.SelectCaseWaiter w, boolean isSend) {
        synchronized (this) {
            (isSend ? sendWaiters : recvWaiters).remove(w);
        }
    }
}
