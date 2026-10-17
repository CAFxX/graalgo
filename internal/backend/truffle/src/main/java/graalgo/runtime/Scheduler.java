package graalgo.runtime;

import graalgo.values.GoFunc;

/**
 * Compile-time scheduler-glue intrinsic surface (WS G, GG-0020; bridge B-8,
 * inventoried in {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This class exists so the truffle module compiles against the scheduler
 * surface despite the truffle&#8596;runtime compile cycle (the runtime module
 * compiles the joint round first; the truffle module then compiles this
 * same-FQN class against it). The real scheduler glue lives in the runtime
 * module ({@code graalgo.runtime.Scheduler} in {@code internal/backend/runtime}).
 * At launch, {@code internal/backend/graalgo-run} puts
 * {@code runtime/out/classes} <em>before</em> {@code truffle/out/classes} on
 * the classpath, so the real implementation shadows this class — this stub
 * is only ever loaded if the launcher classpath order is violated (a launch
 * bug). Every method throws {@link UnsupportedOperationException}. (Goexit/
 * defer/panic machinery itself is worker-F's, WS F, GG-0019.)
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
