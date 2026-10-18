package graalgo.runtime.sync;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

import java.util.ArrayDeque;
import java.util.Deque;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Scheduler-aware once ({@code sync.Once} semantics): {@code doOnce}
 * runs the action exactly once; concurrent callers park scheduler-aware
 * until the winner finishes (slow path). Fast path is a single volatile
 * read (done flag).
 *
 * <p>Must be called from a goroutine (scheduler-managed virtual thread);
 * the slow path parks via the scheduler and throws
 * {@code IllegalStateException} on a non-scheduler thread. The fast path
 * ({@code state == DONE}) is safe from any thread.
 */
public final class GoOnce implements Copyable {
    private static final int IDLE = 0;
    private static final int RUNNING = 1;
    private static final int DONE = 2;

    private final AtomicInteger state = new AtomicInteger(IDLE);
    private final Deque<GoroutineHandle> waiters = new ArrayDeque<>();
    private final Object waitLock = new Object();

    /**
     * Runs {@code action} exactly once. Concurrent callers block until
     * the in-progress call completes, then return without running it.
     */
    public void doOnce(Runnable action) {
        if (state.get() == DONE) {
            return;
        }
        if (state.compareAndSet(IDLE, RUNNING)) {
            // P2-M10 reviewer P2-3: resolve the scheduler handle BEFORE the
            // state transition. If the caller is not a scheduler goroutine,
            // current() throws here — before state becomes DONE — instead of
            // stranding parked waiters with state == DONE.
            SchedulerCore sched = SchedulerCore.current();
            Throwable thrown = null;
            try {
                action.run();
            } catch (Throwable t) {
                thrown = t;
            }
            synchronized (waitLock) {
                state.set(DONE);
                while (!waiters.isEmpty()) {
                    sched.unpark(waiters.pollFirst());
                }
            }
            if (thrown != null) {
                if (thrown instanceof RuntimeException re) {
                    throw re;
                }
                throw new RuntimeException(thrown);
            }
            return;
        }
        // Slow path: someone else is running it; park until DONE.
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle h = sched.currentGoroutine();
        synchronized (waitLock) {
            if (state.get() == DONE) {
                return;
            }
            waiters.addLast(h);
        }
        sched.parkCurrent(null);
    }

    /** For tests. */
    public boolean isDone() {
        return state.get() == DONE;
    }

    /**
     * Go value-copy semantics (RV18-P1-16): a copied Once is a fresh,
     * not-done one — a Do through one copy is never observed through
     * another, like gc's value copy.
     */
    @Override
    public GoOnce freshCopy() {
        return new GoOnce();
    }
}
