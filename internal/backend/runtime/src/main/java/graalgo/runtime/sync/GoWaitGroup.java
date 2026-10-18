package graalgo.runtime.sync;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

import java.util.ArrayDeque;
import java.util.Deque;

/**
 * Scheduler-aware wait group ({@code sync.WaitGroup} semantics).
 *
 * <p>{@code Wait} parks scheduler-aware until the counter reaches zero.
 * A {@code Wait} that starts when the counter is already zero returns
 * immediately (gc parity). Misuse (negative counter, Add concurrent with
 * Wait racing to zero) panics like gc — here simplified to
 * IllegalStateException on negative counter.
 */
public final class GoWaitGroup implements Copyable {
    private int counter;
    private final Deque<GoroutineHandle> waiters = new ArrayDeque<>();

    /** Adds delta to the counter (may be negative). */
    public synchronized void add(int delta) {
        int next = counter + delta;
        if (next < 0) {
            throw new IllegalStateException("sync: negative WaitGroup counter");
        }
        counter = next;
        if (next == 0) {
            // Wake all waiters; done() does the same.
            SchedulerCore sched = SchedulerCore.current();
            while (!waiters.isEmpty()) {
                sched.unpark(waiters.pollFirst());
            }
        }
    }

    /** Decrements the counter by one. */
    public void done() {
        add(-1);
    }

    /** Parks scheduler-aware until the counter is zero. */
    public void await() {
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle h;
        synchronized (this) {
            if (counter == 0) {
                return;
            }
            h = sched.currentGoroutine();
            waiters.addLast(h);
        }
        sched.parkCurrent(null);
    }

    /** For tests. */
    public synchronized int count() {
        return counter;
    }

    /**
     * Go value-copy semantics (RV18-P1-16): a copied WaitGroup is a fresh,
     * zero one — operations through one copy are never observed through
     * another, like gc's value copy.
     */
    @Override
    public GoWaitGroup freshCopy() {
        return new GoWaitGroup();
    }
}
