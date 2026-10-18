package graalgo.runtime.sync;

import graalgo.runtime.Panic;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoString;

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
 * <p>Go semantics: non-reentrant; {@code unlock} of an unlocked mutex
 * raises a Go panic with gc's message text ("sync: unlock of unlocked
 * mutex"). gc itself reports a fatal error here (unrecoverable); the
 * recoverable-panic divergence is documented in ADR-0002 D4. Unlocking
 * from a different goroutine than the locker is allowed (as in gc).
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

    /** Releases the lock, waking the longest-waiting contender (FIFO). */
    public void unlock() {
        GoroutineHandle next;
        synchronized (this) {
            if (!locked) {
                // RV18-P1-10: misuse is a Go panic with gc's message text,
                // never a host exception (previously IllegalStateException,
                // which escaped as "graalgo-run: execution failed").
                // Checked before touching the scheduler: misuse is a pure
                // state condition, and the valid path is unreachable
                // without a scheduler (lock() fail-fasts first), so this
                // never masks the scheduler fail-fast.
                Panic.throwPanic(GoString.fromUtf8("sync: unlock of unlocked mutex"));
            }
            next = waiters.pollFirst();
            if (next == null) {
                locked = false;
            }
            // else: locked stays true, ownership transfers to next.
        }
        if (next != null) {
            SchedulerCore.current().unpark(next);
        }
    }

    /** For tests: whether the lock is currently held. */
    public synchronized boolean isLocked() {
        return locked;
    }
}
