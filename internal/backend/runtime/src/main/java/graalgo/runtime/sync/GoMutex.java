package graalgo.runtime.sync;

import graalgo.runtime.GoFatalError;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

import java.util.ArrayDeque;
import java.util.Deque;

/**
 * Scheduler-aware mutex ({@code sync.Mutex} semantics) for the Phase-2
 * runtime.
 *
 * <p>Blocking is scheduler-aware: a contender that cannot acquire the lock
 * parks via {@link SchedulerCore#parkCurrent} and is re-admitted through
 * the drainer's WAKE lane. Raw Java blocking ({@code synchronized} wait,
 * {@code LockSupport.park} without a ticket) is banned here per P2-D4 —
 * it would bypass the in-flight cap accounting.
 *
 * <p>Go semantics: non-reentrant; {@code unlock} of an unlocked mutex is a
 * Go <em>fatal error</em> ({@code fatal error: sync: unlock of unlocked
 * mutex}, exit 2 — gc calls {@code throw()}, not {@code panic()}, so it is
 * unrecoverable); unlocking from a different goroutine than the locker is
 * allowed (as in gc). Misuse detection does not depend on a scheduler
 * being installed.
 *
 * <p>verifies: GG-SCHED-0004 (scheduler-aware blocking).
 */
public final class GoMutex {
    private boolean locked;
    private final Deque<GoroutineHandle> waiters = new ArrayDeque<>();

    /** Acquires the lock, parking scheduler-aware on contention. */
    public void lock() {
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle h;
        synchronized (this) {
            if (!locked) {
                locked = true;
                return;
            }
            h = sched.currentGoroutine();
            waiters.addLast(h);
        }
        // Park until unlock() unparks us. The drainer matches UNPARK by
        // handle even if it arrives before we park (stashed) — no lost wakeup.
        sched.parkCurrent(null);
        // On resume we own the lock (unlock() transferred it to us).
    }

    /**
     * Releases the lock, waking the longest-waiting contender (FIFO).
     *
     * <p>Unlock of an unlocked mutex is a Go fatal error (P1-9), checked
     * before touching the scheduler so misuse is reported even where no
     * scheduler is installed.
     */
    public void unlock() {
        GoroutineHandle next;
        synchronized (this) {
            if (!locked) {
                throw new GoFatalError("sync: unlock of unlocked mutex");
            }
            next = waiters.pollFirst();
            if (next == null) {
                locked = false;
                return;
            }
            // else: locked stays true, ownership transfers to next.
        }
        SchedulerCore.current().unpark(next);
    }

    /** For tests: whether the lock is currently held. */
    public synchronized boolean isLocked() {
        return locked;
    }
}
