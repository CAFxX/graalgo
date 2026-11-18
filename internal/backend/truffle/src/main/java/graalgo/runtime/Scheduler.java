package graalgo.runtime;

import graalgo.values.GoFunc;

/**
 * Scheduler-glue intrinsic surface (WS G, GG-0020): the entry points the
 * AST nodes call for {@code go}, yield and park. Signatures are the frozen
 * ones ({@code FreezeSignatureTest}); bodies delegate to
 * {@link graalgo.sched.SchedulerCore}.
 */
public final class Scheduler {
    private Scheduler() {}

    /**
     * Create a new goroutine running {@code fn} (no-arg functions in
     * P2-M1; argument binding arrives with the call protocol in P2-M2+).
     */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void spawn(GoFunc fn) {
        graalgo.sched.SchedulerCore.current().spawnGoFunc(fn);
    }

    /** Voluntarily yield the processor (quantum park). */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void yield() {
        graalgo.sched.SchedulerCore.current().yieldCurrent();
    }

    /**
     * Park the current goroutine indefinitely. The wake action {@code wake}
     * runs on the <b>goroutine's own vthread</b> immediately after resume
     * (before {@code parkCurrent} returns) — it is not run on the waker
     * thread. Wake via {@code SchedulerCore.unpark} (channels in P2-M2).
     */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void park(Runnable wake) {
        graalgo.sched.SchedulerCore.current().parkCurrent(wake);
    }

    /** Number of live goroutines (for the netpoller contract). */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static int runnableCount() {
        return graalgo.sched.SchedulerCore.current().runnableCount();
    }
}
