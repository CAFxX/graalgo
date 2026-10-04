package usched;

import java.util.ArrayDeque;

/**
 * Go channel model wired to the user-space scheduler (spike 2b).
 *
 * Every blocking park goes through UserScheduler with an armed ticket:
 * the waiter is registered (visible to wakers) only AFTER armPark(), and
 * the waker dequeues under the channel lock then wins the wakeup via the
 * armed CAS — so no wakeup is lost or duplicated. Parking always happens
 * OUTSIDE the channel monitor (a vthread must never park holding a lock
 * the drainer or a waker needs).
 *
 * Faithful points: FIFO waiter queues, direct handoff when a counterpart
 * waits, close semantics (send-on-closed panics, recv-on-closed drains then
 * zero value), fixed buffer.
 */
public final class GoChannel<T> {
    private final Object[] buf;
    private final int cap;
    private int head, tail, size;
    private boolean closed = false;

    private final ArrayDeque<Waiter<T>> sendQ = new ArrayDeque<>();
    private final ArrayDeque<Waiter<T>> recvQ = new ArrayDeque<>();
    private final ArrayDeque<SelSignal> selWaiters = new ArrayDeque<>();

    private static final java.util.concurrent.atomic.AtomicLong nextSelId =
            new java.util.concurrent.atomic.AtomicLong(1);
    private final long selId = nextSelId.getAndIncrement();
    public long selId() { return selId; }

    /** Select waiters are signaled on any state change (spurious wakeups OK). */
    public interface SelSignal { void signal(); }

    static final class Waiter<T> {
        final Goroutine g;
        final long ticket;
        T elem;
        volatile boolean done = false;    // handoff completed
        volatile boolean aborted = false; // woken by close()
        Waiter(Goroutine g, long ticket) { this.g = g; this.ticket = ticket; }
    }

    public GoChannel(int capacity) {
        this.cap = capacity;
        this.buf = new Object[capacity];
    }

    public GoChannel() { this(0); }

    public void send(T v) {
        UserScheduler us = UserScheduler.instance();
        Goroutine g;
        Waiter<T> p;
        synchronized (this) {
            if (closed) throw new GoPanic("send on closed channel");
            Waiter<T> r = recvQ.pollFirst();
            if (r != null) { // direct handoff to waiting receiver
                r.elem = v;
                r.done = true;
                signalSelWaiters();
                us.wakeup(r.g, r.ticket);
                return;
            }
            if (size < cap) {
                buf[tail] = v;
                tail = (tail + 1) % cap;
                size++;
                signalSelWaiters();
                return;
            }
            g = Goroutine.current();
            long t = us.armPark(g);
            p = new Waiter<>(g, t);
            p.elem = v;
            sendQ.addLast(p);
            // A newly arrived sender makes recv cases ready: wake select
            // waiters so they re-check (else a select-blocked receiver and a
            // parked sender deadlock).
            signalSelWaiters();
        }
        us.parkIdle(g, p.ticket);
        if (p.aborted) throw new GoPanic("send on closed channel");
    }

    @SuppressWarnings("unchecked")
    public T recv() {
        UserScheduler us = UserScheduler.instance();
        Goroutine g;
        Waiter<T> p;
        synchronized (this) {
            if (size > 0) {
                T v = (T) buf[head];
                buf[head] = null;
                head = (head + 1) % cap;
                size--;
                Waiter<T> s = sendQ.pollFirst();
                if (s != null) { // refill from woken sender
                    buf[tail] = s.elem;
                    tail = (tail + 1) % cap;
                    size++;
                    s.done = true;
                    us.wakeup(s.g, s.ticket);
                }
                signalSelWaiters();
                return v;
            }
            Waiter<T> s = sendQ.pollFirst();
            if (s != null) { // unbuffered direct handoff
                T v = s.elem;
                s.done = true;
                us.wakeup(s.g, s.ticket);
                signalSelWaiters();
                return v;
            }
            if (closed) return null; // Go: zero value after drain
            g = Goroutine.current();
            long t = us.armPark(g);
            p = new Waiter<>(g, t);
            recvQ.addLast(p);
            // Symmetric to send(): a newly arrived receiver makes send cases
            // ready for select waiters.
            signalSelWaiters();
        }
        us.parkIdle(g, p.ticket);
        if (p.aborted) return null; // woken by close: zero value
        return p.elem;
    }

    public void close() {
        UserScheduler us = UserScheduler.instance();
        synchronized (this) {
            if (closed) throw new GoPanic("close of closed channel");
            closed = true;
            Waiter<T> w;
            while ((w = recvQ.pollFirst()) != null) {
                w.done = true; // elem null = zero value
                us.wakeup(w.g, w.ticket);
            }
            while ((w = sendQ.pollFirst()) != null) {
                w.aborted = true;
                us.wakeup(w.g, w.ticket);
            }
            signalSelWaiters();
        }
    }

    /** Non-blocking send; false if it would block. */
    public boolean trySend(T v) {
        UserScheduler us = UserScheduler.instance();
        synchronized (this) {
            if (closed) throw new GoPanic("send on closed channel");
            Waiter<T> r = recvQ.pollFirst();
            if (r != null) {
                r.elem = v; r.done = true;
                signalSelWaiters();
                us.wakeup(r.g, r.ticket);
                return true;
            }
            if (size < cap) {
                buf[tail] = v; tail = (tail + 1) % cap; size++;
                signalSelWaiters();
                return true;
            }
            return false;
        }
    }

    /** Non-blocking recv. */
    public RecvResult<T> tryRecv() {
        UserScheduler us = UserScheduler.instance();
        synchronized (this) {
            if (size > 0) {
                @SuppressWarnings("unchecked") T v = (T) buf[head];
                buf[head] = null; head = (head + 1) % cap; size--;
                Waiter<T> s = sendQ.pollFirst();
                if (s != null) {
                    buf[tail] = s.elem; tail = (tail + 1) % cap; size++;
                    s.done = true;
                    us.wakeup(s.g, s.ticket);
                }
                signalSelWaiters();
                return new RecvResult<>(v, true);
            }
            Waiter<T> s = sendQ.pollFirst();
            if (s != null) {
                T v = s.elem; s.done = true;
                us.wakeup(s.g, s.ticket);
                signalSelWaiters();
                return new RecvResult<>(v, true);
            }
            if (closed) return new RecvResult<>(null, true);
            return new RecvResult<>(null, false);
        }
    }

    public synchronized boolean isClosed() { return closed; }

    public record RecvResult<T>(T value, boolean ok) {}

    // ---- select support ----
    synchronized void addSelectWaiter(SelSignal w) { selWaiters.addLast(w); }
    synchronized void removeSelectWaiter(SelSignal w) { selWaiters.remove(w); }

    /** Must be called with the monitor held. */
    private void signalSelWaiters() {
        if (selWaiters.isEmpty()) return;
        // Copy: signal() -> wakeup() is non-blocking, but keep iteration safe.
        for (SelSignal w : selWaiters.toArray(new SelSignal[0])) w.signal();
    }
}
