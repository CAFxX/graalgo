package graalgo.sched;

import graalgo.jfr.JfrEvents;
import graalgo.jfr.ParkReason;
import jdk.jfr.Recording;

/**
 * P2-M9: direct microbenchmark of the JFR park-hook cost (done-gate 4).
 *
 * <p>Replaces the old {@code testYieldThroughputSmoke} throughput-ratio
 * test (removed from {@code JfrEventsTest}): a throughput ratio cannot
 * measure a ~0.5% effect under ±10% scheduling noise on a shared box —
 * the old test flaked ~40% in-suite with ratios swinging 0.50–1.65, and
 * no amount of interleaving fixes a fundamentally unmeasurable quantity.
 * This test measures the property directly and deterministically: the
 * per-park cost of {@code notePark}+{@code parkResumed} (the hooks on the
 * scheduler hot path), with a JFR recording enabled vs disabled.
 *
 * <p>No scheduler, no vthreads, no drainer: just the hooks in a tight
 * loop, so box load cannot inject scheduling noise. The budget (500ns)
 * The budget (1000ns, ~2x the measured ~450ns enabled cost) is generous
 * enough to never flake, tight enough to catch any real regression
 * (per-park event emission costs microseconds; a slow disabled guard
 * would show in the delta). Fault-injection verified: a 2us sleep in
 * {@code parkResumed} fails this test (6612ns > budget — verified
 * 2026-10-05, then reverted).
 */
public final class JfrParkCostTest {

    /**
     * Generous upper bound, set from measured reality (2026-10-05:
     * enabled ~420-510ns/park on this VM): ~2x headroom, still catches
     * any real regression (per-park event emission costs microseconds).
     */
    private static final double BUDGET_NANOS_PER_PARK = 1000.0;
    /** Upper bound on the enabled-vs-disabled delta (measured ~300ns). */
    private static final double DELTA_BUDGET_NANOS_PER_PARK = 800.0;

    private static final int WARMUP = 200_000;
    private static final int ITERS = 500_000;
    private static final int ROUNDS = 7;

    /**
     * Globally reachable probe handle. The measured hooks write volatile
     * fields; if the handle never escapes the measuring method, the JIT
     * scalar-replaces it and the disabled loop collapses to ~0ns (observed
     * 3.2E-4ns/park) — a bogus baseline. A static-volatile-reachable
     * handle forces real volatile writes.
     */
    private static volatile GoroutineHandle probe;

    /**
     * Times {@code notePark}+{@code parkResumed} per park, in nanoseconds.
     * {@code parkSeq} is pinned to a non-sampled value so every iteration
     * takes the common early-return path (1023/1024 of real parks); the
     * 1-in-1024 emit path is covered separately by the trickle test and
     * its JFR-machinery cost is out of scope for this budget.
     */
    private static double measureOnce() {
        GoroutineHandle h = probe;
        h.parkSeq.set(1L);
        for (int i = 0; i < ITERS; i++) {
            JfrEvents.notePark(h, ParkReason.QUANTUM);
            JfrEvents.parkResumed(h);
        }
        // Timed run (second loop so the first also serves as warmup).
        h.parkSeq.set(1L);
        long t0 = System.nanoTime();
        for (int i = 0; i < ITERS; i++) {
            JfrEvents.notePark(h, ParkReason.QUANTUM);
            JfrEvents.parkResumed(h);
        }
        return (double) (System.nanoTime() - t0) / ITERS;
    }

    private static double median(double[] xs) {
        java.util.Arrays.sort(xs);
        return xs[xs.length / 2];
    }

    public void testJfrParkPathCost() throws Exception {
        JfrEvents.ensureRegistered();
        probe = new GoroutineHandle(() -> {});
        GoroutineHandle h = probe;

        // Warmup: JIT-compile the hooks.
        h.parkSeq.set(1L);
        for (int i = 0; i < WARMUP; i++) {
            JfrEvents.notePark(h, ParkReason.QUANTUM);
            JfrEvents.parkResumed(h);
        }

        // Disabled baseline (no recording).
        double[] disabled = new double[ROUNDS];
        for (int r = 0; r < ROUNDS; r++) {
            disabled[r] = measureOnce();
        }
        double disabledMed = median(disabled);

        // Enabled: recording with the park event on (as in production).
        double[] enabled = new double[ROUNDS];
        Recording rec = new Recording();
        rec.enable("graalgo.GoroutinePark").with("stackTrace", "false");
        rec.start();
        try {
            for (int r = 0; r < ROUNDS; r++) {
                enabled[r] = measureOnce();
            }
        } finally {
            rec.stop();
            rec.close();
        }
        double enabledMed = median(enabled);

        System.out.println("JFR park-hook cost: disabled=" + disabledMed
                + "ns/park enabled=" + enabledMed + "ns/park");
        assertTrue(enabledMed < BUDGET_NANOS_PER_PARK,
                "JFR enabled park-hook cost blew the budget: " + enabledMed + "ns/park");
        assertTrue(enabledMed - disabledMed < DELTA_BUDGET_NANOS_PER_PARK,
                "JFR enabled-vs-disabled delta blew the budget: "
                + (enabledMed - disabledMed) + "ns/park");
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }
}
