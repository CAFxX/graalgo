package graalgo.runtime;

import java.util.ArrayDeque;
import java.util.Deque;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

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
 * peek and the handoff just makes trySend return false).
 */
public final class GoChan implements ChanOps.Chan {
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
        return !recvWaiters.isEmpty() || count < buf.length;
    }

    /**
     * Non-mutating readiness for select polling: would {@link #tryRecv}
     * succeed (non-null)?
     */
    boolean canRecv() {
        purgeClaimed(sendWaiters);
        return !sendWaiters.isEmpty() || count > 0 || closed;
    }

    /** Drops already-claimed select waiters from a queue head. */
    private void purgeClaimed(Deque<ChanWaiter> q) {
        while (!q.isEmpty()) {
            ChanWaiter w = q.peekFirst();
            if (w instanceof SelectOp.SelectCaseWaiter sw && sw.isClaimed()) {
                q.pollFirst();
            } else {
                break;
            }
        }
    }

    /**
     * Attempts a non-blocking send under the monitor. Returns true if
     * the send completed (direct handoff or buffered); false if it would
     * block. Never parks.
     */
    boolean trySend(Object v, SchedulerCore sched) {
        ChanWaiter r = pollAccepting(recvWaiters);
        if (r != null) {
            deliverToReceiver(r, v, true, sched);
            return true;
        }
        if (count < buf.length) {
            buf[tail] = v;
            tail = (tail + 1) % buf.length;
            count++;
            return true;
        }
        return false;
    }

    /**
     * Attempts a non-blocking receive under the monitor. Returns the
     * result, or null if it would block. {@code (zeroValue, false)} once
     * closed and drained. Never parks.
     */
    RecvResult tryRecv(SchedulerCore sched) {
        ChanWaiter s = pollAccepting(sendWaiters);
        if (s != null) {
            return new RecvResult(takeFromSender(s, sched), true);
        }
        if (count > 0) {
            Object v = buf[head];
            buf[head] = null;
            head = (head + 1) % buf.length;
            count--;
            return new RecvResult(v, true);
        }
        if (closed) {
            return new RecvResult(zeroValue, false);
        }
        return null;
    }

    /**
     * Dequeues the head waiter, dropping select waiters already claimed
     * by another channel. Returns null if the queue is empty.
     */
    private ChanWaiter pollAccepting(Deque<ChanWaiter> q) {
        while (!q.isEmpty()) {
            ChanWaiter w = q.pollFirst();
            if (w.accept()) {
                return w;
            }
        }
        return null;
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
        SendWaiter w;
        synchronized (this) {
            if (closed) {
                Panic.throwPanic(GoStrings.of("send on closed channel"));
            }
            if (trySend(v, sched)) {
                return;
            }
            w = new SendWaiter(sched.currentGoroutine(), v);
            sendWaiters.addLast(w);
        }
        // Outside the monitor: scheduler-aware park (P2-D4: never blocks
        // the carrier). A waker that dequeued us before we park has its
        // UNPARK stashed by the drainer — no wakeup lost.
        sched.parkCurrent(null);
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
        RecvWaiter w;
        synchronized (this) {
            RecvResult r = tryRecv(sched);
            if (r != null) {
                return r;
            }
            w = new RecvWaiter(sched.currentGoroutine());
            recvWaiters.addLast(w);
        }
        sched.parkCurrent(null);
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
        synchronized (this) {
            if (closed) {
                Panic.throwPanic(GoStrings.of("close of closed channel"));
            }
            closed = true;
            for (ChanWaiter r : recvWaiters) {
                if (r instanceof RecvWaiter rw) {
                    rw.value = zeroValue;
                    rw.ok = false;
                    sched.unpark(rw.handle);
                } else if (r.accept()) {
                    // Select recv case: closed is always ready.
                    SelectOp.SelectCaseWaiter sw = (SelectOp.SelectCaseWaiter) r;
                    sw.op.setResult(sw.caseIndex, zeroValue, false);
                    sched.unpark(sw.handle);
                }
            }
            recvWaiters.clear();
            for (ChanWaiter s : sendWaiters) {
                if (s instanceof SendWaiter sw) {
                    sw.panicOnWake = true;
                    sched.unpark(sw.handle);
                } else if (s.accept()) {
                    // Select send case on a channel being closed: poison.
                    // The select panics on resume ("send on closed
                    // channel"); see SelectOp. A select that already
                    // completed via another case fails accept() and is
                    // simply dropped here.
                    SelectOp.SelectCaseWaiter sw = (SelectOp.SelectCaseWaiter) s;
                    sw.op.setPoison(sw.caseIndex);
                    sched.unpark(sw.handle);
                }
            }
            sendWaiters.clear();
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
