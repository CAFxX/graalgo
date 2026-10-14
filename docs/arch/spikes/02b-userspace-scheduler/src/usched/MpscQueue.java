package usched;

import java.util.concurrent.atomic.AtomicReference;
import java.util.concurrent.locks.LockSupport;

/**
 * Minimal MPSC queue for scheduler events. take() spins briefly before
 * parking, to test whether deep thread sleep (C-states) explains wakeup
 * latency on this box. Not for production use.
 */
public final class MpscQueue<T> {
    private static final class Node<T> {
        T item;
        volatile Node<T> next;
    }

    private final AtomicReference<Node<T>> head = new AtomicReference<>();
    private volatile Node<T> tail;
    volatile Thread taker; // package-private: UserScheduler sets for cross-queue wakeup

    public MpscQueue() {
        Node<T> stub = new Node<>();
        head.set(stub);
        tail = stub;
    }

    public void offer(T item) {
        Node<T> n = new Node<>();
        n.item = item;
        Node<T> prev = head.getAndSet(n);
        prev.next = n; // volatile store: publishes item
        Thread t = taker;
        if (t != null) {
            taker = null;
            LockSupport.unpark(t);
        }
    }

    /** Non-blocking poll; returns null if empty. */
    public T poll() {
        Node<T> t = tail;
        Node<T> next = t.next;
        if (next == null) return null;
        tail = next;
        T item = next.item;
        next.item = null;
        return item;
    }

    public T take() throws InterruptedException {
        Thread me = Thread.currentThread();
        taker = me;
        Node<T> t = tail;
        for (;;) {
            Node<T> next = t.next; // volatile load
            if (next != null) {
                tail = next;
                T item = next.item;
                next.item = null;
                return item;
            }
            if (me.isInterrupted()) throw new InterruptedException();
            long spinEnd = System.nanoTime() + 50_000; // 50 us spin
            while (t.next == null && System.nanoTime() < spinEnd) {
                Thread.onSpinWait();
            }
            if (t.next == null) {
                taker = me;
                LockSupport.park();
            }
            t = tail;
        }
    }
}
