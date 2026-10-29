package graalgo.runtime.sync;

import java.util.concurrent.locks.ReentrantLock;

/**
 * The pin-section lock (fix-redmaster-r43).
 *
 * <p>sync.Pool's pin sections (and sync/atomic's first-store sections)
 * serialize on this lock: procPin takes it, procUnpin releases it. The
 * lock lives in the runtime module (not interop) so the scheduler can
 * test it: a goroutine holding it must never park for a quantum yield
 * (see SchedulerCore.pollCurrentThread) — parking while holding it
 * deadlocks against the drainer re-admission path, and in gc pinning
 * disables preemption anyway, so deferring the yield past the section
 * is the faithful semantics, not a workaround.
 *
 * <p>Reentrant: pin sections never nest in practice, but reentrancy
 * makes accidental nesting safe rather than fatal.
 */
public final class PinLock {
    private PinLock() {}

    public static final ReentrantLock LOCK = new ReentrantLock();

    /** True when the calling thread holds the pin lock. */
    public static boolean isHeldByCurrentThread() {
        return LOCK.isHeldByCurrentThread();
    }
}
