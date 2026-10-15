package graalgo.runtime.sync;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

import java.util.ArrayDeque;
import java.util.Deque;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Scheduler-aware semaphore for {@code runtime_Semacquire}/
 * {@code runtime_Semrelease} (Go's sync primitive).
 *
 * <p>Go's semaphore semantics (src/runtime/sema.go):
 * <ul>
 *   <li>{@code Semacquire(s *uint32)}: wait until {@code *s > 0}, then
 *       atomically decrement it.
 *   <li>{@code Semrelease(s *uint32)}: atomically increment {@code *s},
 *       wake one waiter if any.
 * </ul>
 *
 * <p>The count lives in Go memory (accessed via the {@link IntCell}
 * abstraction); the waiter queue lives here, keyed by the cell's
 * identity. Blocking is scheduler-aware: contenders park via
 * {@link SchedulerCore#parkCurrent} and are re-admitted through the
 * drainer's WAKE lane, matching {@link GoMutex}.
 *
 * <p>Ownership transfer: when a releaser wakes a waiter, ownership of
 * the semaphore transfers directly (no count increment/decrement
 * dance). This avoids the race where a third goroutine could steal
 * the count between release and waiter wakeup.
 *
 * <p>verifies: real-sync semaphore support.
 */
public final class GoSemaphore {
    private GoSemaphore() {}

    /** Per-semaphore waiter state, keyed by the IntCell identity. */
    private static final Map<IntCell, Deque<GoroutineHandle>> WAITERS =
            new ConcurrentHashMap<>();

    /** Global lock for atomic count + waiter-queue updates. */
    private static final Object LOCK = new Object();

    /**
     * Acquires the semaphore: waits until the count is positive, then
     * atomically decrements it.
     *
     * @param cell the {@code *uint32} semaphore count
     */
    public static void acquire(IntCell cell) {
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle h;
        synchronized (LOCK) {
            // Easy case: count > 0, decrement and return.
            int c = cell.get();
            if (c > 0) {
                cell.set(c - 1);
                return;
            }
            // Hard case: enqueue as waiter.
            h = sched.currentGoroutine();
            WAITERS.computeIfAbsent(cell, k -> new ArrayDeque<>()).addLast(h);
            // Re-check after enqueue to avoid missed wakeup: a release
            // between our get() and enqueue would have either incremented
            // (no waiters at the time) or woken us (we're now in the queue).
            // If count > 0 now, it means a release incremented without
            // waking us (we weren't in the queue yet) — take it.
            c = cell.get();
            if (c > 0) {
                Deque<GoroutineHandle> q = WAITERS.get(cell);
                if (q != null) {
                    q.remove(h);
                    if (q.isEmpty()) {
                        WAITERS.remove(cell);
                    }
                }
                cell.set(c - 1);
                return;
            }
        }
        // Park until release() unparks us. The drainer matches UNPARK by
        // handle even if it arrives before we park (stashed) — no lost wakeup.
        // On resume, ownership has transferred to us (releaser did not
        // increment the count; we own the wakeup ticket).
        sched.parkCurrent(null);
    }

    /**
     * Releases the semaphore: wakes one waiter if any (transferring
     * ownership), otherwise atomically increments the count.
     *
     * @param cell the {@code *uint32} semaphore count
     */
    public static void release(IntCell cell) {
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle next;
        synchronized (LOCK) {
            Deque<GoroutineHandle> q = WAITERS.get(cell);
            if (q != null && !q.isEmpty()) {
                // Transfer ownership to the waiter: dequeue and wake.
                // Do NOT increment the count — the waiter owns the
                // semaphore now (it will not decrement on wakeup).
                next = q.pollFirst();
                if (q.isEmpty()) {
                    WAITERS.remove(cell);
                }
            } else {
                // No waiters: increment the count.
                int c = cell.get();
                cell.set(c + 1);
                next = null;
            }
        }
        if (next != null) {
            sched.unpark(next);
        }
    }

    /**
     * Acquires the semaphore for WaitGroup. Currently identical to
     * {@link #acquire}; the synctestDurable flag is ignored as graalgo
     * does not implement synctest.
     *
     * @param cell the {@code *uint32} semaphore count
     */
    public static void acquireWaitGroup(IntCell cell) {
        acquire(cell);
    }
}
