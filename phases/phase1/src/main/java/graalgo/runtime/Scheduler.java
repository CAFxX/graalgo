package graalgo.runtime;

import graalgo.values.GoFunc;

/**
 * Frozen scheduler-glue intrinsic surface (WS G, GG-0020, STUB).
 *
 * <p>Phase 2 owns goroutine scheduling; these signatures are frozen now
 * so Phase-2 code and the Phase-1 AST build against the same contract.
 * Every method throws {@link UnsupportedOperationException} with a
 * Phase-2 message until Phase 2 implements it. (Goexit/defer/panic
 * machinery itself is worker-F's, WS F, GG-0019.)
 */
public final class Scheduler {
    private Scheduler() {}

    /**
     * STUB — create a new goroutine running {@code fn}. Phase 2.
     */
    public static void spawn(GoFunc fn) {
        throw new UnsupportedOperationException(
                "Phase 2: goroutine creation needs the scheduler (GG-0020)");
    }

    /**
     * STUB — voluntarily yield the processor. Phase 2.
     */
    public static void yield() {
        throw new UnsupportedOperationException(
                "Phase 2: scheduling needs the scheduler (GG-0020)");
    }

    /**
     * STUB — park the current goroutine until {@code wake} runs. Phase 2.
     */
    public static void park(Runnable wake) {
        throw new UnsupportedOperationException(
                "Phase 2: parking needs the scheduler (GG-0020)");
    }

    /**
     * STUB — number of runnable goroutines (for the netpoller contract).
     * Phase 2.
     */
    public static int runnableCount() {
        throw new UnsupportedOperationException(
                "Phase 2: scheduling needs the scheduler (GG-0020)");
    }
}
