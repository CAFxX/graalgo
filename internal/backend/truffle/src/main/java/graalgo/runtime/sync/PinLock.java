package graalgo.runtime.sync;

import java.util.concurrent.locks.ReentrantLock;

/**
 * The pin-section lock (fix-redmaster-r43).
 *
 * <p>sync.Pool's pin sections (and sync/atomic's first-store sections)
 * serialize on this lock: procPin takes it, procUnpin releases it. The
 * lock lives in the core module (not interop) so the scheduler can
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

    /**
     * Takes the pin lock for Go code ({@code procPin}, {@code runtime.GC}). Contended, the wait
     * is Truffle-blocked (playbook §10): a thread-local action — cancellation or hard exit of
     * the context — reaches the waiter, which would otherwise wait forever on a lock whose
     * holder the teardown unwound.
     */
    public static void lockForGo(com.oracle.truffle.api.nodes.Node node) {
        if (!LOCK.tryLock()) {
            com.oracle.truffle.api.TruffleSafepoint.setBlockedThreadInterruptible(node,
                    ReentrantLock::lockInterruptibly, LOCK);
        }
    }

    /**
     * Drops every hold of the calling thread: a goroutine that ends — normally never inside a
     * pin section, but a context's cancellation or hard exit unwinds it from any Truffle poll,
     * and no deferred {@code procUnpin} runs then — must not leave the lock held by its
     * thread (in carrier mode a carrier that outlives the goroutine, or exits holding it:
     * every later pin section of the process would block forever).
     */
    public static void releaseAllHeldByCurrentThread() {
        while (LOCK.isHeldByCurrentThread()) {
            LOCK.unlock();
        }
    }

    /** True when the calling thread holds the pin lock. */
    public static boolean isHeldByCurrentThread() {
        return LOCK.isHeldByCurrentThread();
    }
}
