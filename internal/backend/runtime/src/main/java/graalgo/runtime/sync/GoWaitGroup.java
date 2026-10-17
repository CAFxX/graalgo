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
public final class GoWaitGroup {
    private int counter;
    private final Deque<GoroutineHandle> waiters = new ArrayDeque<>();

    /** Adds delta to the counter (may be negative). */
    public synchronized void add(int delta) {
        int next = counter + delta;
        if (next < 0) {
            throw new IllegalStateException("sync: negative WaitGroup counter");
        }
        // R2 P1-2 (mirrors the GoOnce P2-M10 fix): resolve the scheduler
        // BEFORE mutating the counter. If the caller is not on a
        // scheduler (current() throws), the throw lands before
        // counter = next — instead of stranding parked waiters behind a
        // counter that already reads 0. Resolution is conditional on
        // next == 0: Add with a nonzero result needs no scheduler (the
        // unpark loop below is the only scheduler use), so plain
        // setup-time Add calls keep working scheduler-less.
        SchedulerCore sched = next == 0 ? SchedulerCore.current() : null;
        counter = next;
        if (next == 0) {
            // Wake all waiters; done() does the same.
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
}
