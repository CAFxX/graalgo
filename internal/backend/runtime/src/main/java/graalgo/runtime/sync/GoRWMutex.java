package graalgo.runtime.sync;

import graalgo.runtime.Panic;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoString;

import java.util.ArrayDeque;
import java.util.Deque;

/**
 * Scheduler-aware read-write mutex ({@code sync.RWMutex} semantics).
 *
 * <p>Readers proceed concurrently; a writer excludes everyone. To avoid
 * writer starvation, a waiting writer blocks new readers (gc parity:
 * a blocked Lock excludes new readers). All blocking parks
 * scheduler-aware via {@link SchedulerCore#parkCurrent} (P2-D4).
 *
 * <p>Go semantics: RLock of a write-locked mutex blocks; Lock blocks until
 * all readers release; recursive read-locking while a writer waits
 * deadlocks (as in gc — not special-cased).
 */
public final class GoRWMutex {
    private int readers;
    private boolean writer;
    private int waitingWriters;
    private final Deque<GoroutineHandle> readWaiters = new ArrayDeque<>();
    private final Deque<GoroutineHandle> writeWaiters = new ArrayDeque<>();

    /** Shared read lock. Blocks if a writer holds or waits. */
    public void rLock() {
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle h;
        synchronized (this) {
            if (!writer && waitingWriters == 0) {
                readers++;
                return;
            }
            h = sched.currentGoroutine();
            readWaiters.addLast(h);
        }
        sched.parkCurrent(null);
        // On resume: unlock-side accounting already incremented readers.
    }

    /** Releases a read lock. */
    public void rUnlock() {
        SchedulerCore sched = SchedulerCore.current();
        Deque<GoroutineHandle> toWake = new ArrayDeque<>();
        synchronized (this) {
            if (readers <= 0) {
                Panic.throwPanic(GoString.fromUtf8("sync: RUnlock of unlocked RWMutex"));
            }
            readers--;
            if (readers == 0 && waitingWriters > 0) {
                // Hand off to the longest-waiting writer.
                GoroutineHandle w = writeWaiters.pollFirst();
                waitingWriters--;
                writer = true;
                toWake.add(w);
            }
        }
        for (GoroutineHandle h : toWake) {
            sched.unpark(h);
        }
    }

    /** Exclusive write lock. Blocks until no readers and no writer. */
    public void lock() {
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle h;
        synchronized (this) {
            if (!writer && readers == 0) {
                writer = true;
                return;
            }
            h = sched.currentGoroutine();
            writeWaiters.addLast(h);
            waitingWriters++;
        }
        sched.parkCurrent(null);
        // On resume: writer=true was set by the handoff; ownership is ours.
    }

    /** Releases the write lock; wakes all waiting readers first, else next writer. */
    public void unlock() {
        SchedulerCore sched = SchedulerCore.current();
        Deque<GoroutineHandle> toWake = new ArrayDeque<>();
        synchronized (this) {
            if (!writer) {
                Panic.throwPanic(GoString.fromUtf8("sync: Unlock of unlocked RWMutex"));
            }
            writer = false;
            if (!readWaiters.isEmpty()) {
                // Wake all readers; they proceed concurrently.
                while (!readWaiters.isEmpty()) {
                    toWake.add(readWaiters.pollFirst());
                }
                readers += toWake.size();
            } else if (!writeWaiters.isEmpty()) {
                GoroutineHandle w = writeWaiters.pollFirst();
                waitingWriters--;
                writer = true;
                toWake.add(w);
            }
        }
        for (GoroutineHandle h : toWake) {
            sched.unpark(h);
        }
    }
}
