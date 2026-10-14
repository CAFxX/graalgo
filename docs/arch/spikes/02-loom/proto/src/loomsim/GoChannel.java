package loomsim;

import java.util.ArrayDeque;

/**
 * A Go channel model: circular buffer + FIFO waiter queues. Blocking
 * send/recv wait on the channel monitor; a virtual thread in Object.wait()
 * UNMOUNTS its carrier (wait is a blocking operation Loom handles), so the
 * carrier stays free — the property the pinning audit checks.
 *
 * Faithful points: FIFO waiter queues (Go pairs waiters FIFO on unbuffered
 * channels), direct handoff when a counterpart is already waiting, close
 * semantics (send-on-closed panics, recv-on-closed drains then zero value).
 * Buffer never grows (Go semantics).
 */
public final class GoChannel<T> {
    private final Object[] buf;
    private final int cap;
    private int head, tail, size;
    private boolean closed = false;

    private final ArrayDeque<Parker<T>> sendQ = new ArrayDeque<>();
    private final ArrayDeque<Parker<T>> recvQ = new ArrayDeque<>();

    /** Select waiters, signaled on any state change (spurious wakeups OK). */
    private final ArrayDeque<GoSelect.Waiter> selWaiters = new ArrayDeque<>();

    /** Unique id for canonical select lock ordering. */
    private static final java.util.concurrent.atomic.AtomicLong nextSelId = new java.util.concurrent.atomic.AtomicLong(1);
    private final long selId = nextSelId.getAndIncrement();
    long selId() { return selId; }

    /** Go's sudog: one per blocked goroutine, FIFO-queued. */
    static final class Parker<T> {
        T elem;
        boolean ready = false;
    }

    public GoChannel(int capacity) {
        this.cap = capacity;
        this.buf = new Object[capacity];
    }

    public GoChannel() { this(0); }

    public synchronized void send(T v) {
        if (closed) throw new GoPanic("send on closed channel");
        Parker<T> r = recvQ.pollFirst();
        if (r != null) {               // direct handoff to waiting receiver
            r.elem = v; r.ready = true; notifyAll(); signalSelWaiters();
            return;
        }
        if (size < cap) {
            buf[tail] = v; tail = (tail + 1) % cap; size++;
            signalSelWaiters();
            return;
        }
        Parker<T> p = new Parker<>();
        p.elem = v;
        sendQ.addLast(p);
        while (!p.ready) {
            await();
            if (closed && !p.ready) throw new GoPanic("send on closed channel");
        }
    }

    @SuppressWarnings("unchecked")
    public synchronized T recv() {
        if (size > 0) {
            T v = (T) buf[head];
            buf[head] = null; head = (head + 1) % cap; size--;
            Parker<T> s = sendQ.pollFirst();
            if (s != null) {          // refill from woken sender
                buf[tail] = s.elem; tail = (tail + 1) % cap; size++;
                s.ready = true; notifyAll();
            }
            signalSelWaiters();
            return v;
        }
        Parker<T> s = sendQ.pollFirst();
        if (s != null) {              // unbuffered direct handoff
            T v = s.elem; s.ready = true; notifyAll(); signalSelWaiters();
            return v;
        }
        if (closed) return null;      // Go: zero value after drain
        Parker<T> p = new Parker<>();
        recvQ.addLast(p);
        while (!p.ready) await();
        return p.elem;
    }

    public synchronized void close() {
        if (closed) throw new GoPanic("close of closed channel");
        closed = true;
        Parker<T> p;
        while ((p = recvQ.pollFirst()) != null) { p.ready = true; } // elem null = zero value
        notifyAll();
        signalSelWaiters();
    }

    /** Non-blocking send; false if it would block. */
    public synchronized boolean trySend(T v) {
        if (closed) throw new GoPanic("send on closed channel");
        Parker<T> r = recvQ.pollFirst();
        if (r != null) { r.elem = v; r.ready = true; notifyAll(); signalSelWaiters(); return true; }
        if (size < cap) { buf[tail] = v; tail = (tail + 1) % cap; size++; signalSelWaiters(); return true; }
        return false;
    }

    /** Non-blocking recv. */
    public synchronized RecvResult<T> tryRecv() {
        if (size > 0) {
            @SuppressWarnings("unchecked") T v = (T) buf[head];
            buf[head] = null; head = (head + 1) % cap; size--;
            Parker<T> s = sendQ.pollFirst();
            if (s != null) { buf[tail] = s.elem; tail = (tail + 1) % cap; size++; s.ready = true; notifyAll(); }
            signalSelWaiters();
            return new RecvResult<>(v, true);
        }
        Parker<T> s = sendQ.pollFirst();
        if (s != null) { T v = s.elem; s.ready = true; notifyAll(); signalSelWaiters(); return new RecvResult<>(v, true); }
        if (closed) return new RecvResult<>(null, true);
        return new RecvResult<>(null, false);
    }

    public synchronized boolean isClosed() { return closed; }

    public record RecvResult<T>(T value, boolean ok) {}

    // ---- select support (called with the channel monitor held by select) ----
    synchronized void addSelectWaiter(GoSelect.Waiter w) { selWaiters.addLast(w); }
    synchronized void removeSelectWaiter(GoSelect.Waiter w) { selWaiters.remove(w); }

    /** Must be called with the monitor held. Lock order: channel -> waiter. */
    private void signalSelWaiters() {
        if (selWaiters.isEmpty()) return;
        for (GoSelect.Waiter w : selWaiters) w.signal();
    }

    private void await() {
        try { wait(); } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
    }
}
