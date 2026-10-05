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
 * loop, so box load cannot inject scheduling noise. Budgets are
 * machine-relative (multiples of the local {@code nanoTime} cost — the
 * pattern from {@code testDisabledGuardCostNegligible}), because absolute
 * nanosecond budgets do not transfer across machines. Tight enough to
 * catch any real regression (per-park event emission costs microseconds,
 * i.e. tens of nanoCosts). Fault-injection verified: a 2us sleep in
 * {@code parkResumed} fails this test (6612ns >> 8x nanoCost — verified
 * 2026-10-05, then reverted; evidence in CHECKPOINT).
 */
public final class JfrParkCostTest {

    /**
     * Budgets are machine-relative, in units of the local
     * {@code System.nanoTime()} cost (pattern from
     * {@code JfrEventsTest.testDisabledGuardCostNegligible}): absolute
     * nanosecond budgets do not transfer across machines (this VM's
     * nanoTime is ~160ns, ~5x slow; the enabled hook path is dominated by
     * 2x nanoTime). Measured 2026-10-05: enabled ~2.9x nanoCost, delta
     * ~1.9x nanoCost — the budgets below have ~2.7x headroom and still
     * catch any real regression (per-park event emission costs
     * microseconds, i.e. tens of nanoCosts).
     */
    private static final double ENABLED_BUDGET_NANOCOSTS = 8.0;
    private static final double DELTA_BUDGET_NANOCOSTS = 6.0;

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

        // Machine-relative unit: cost of one clock read on this machine.
        for (int i = 0; i < 100_000; i++) {
            System.nanoTime();
        }
        long sink = 0;
        int n = 1_000_000;
        long t0 = System.nanoTime();
        for (int i = 0; i < n; i++) {
            sink += System.nanoTime();
        }
        double nanoCost = (double) (System.nanoTime() - t0) / n;
        if (sink == 0) {
            throw new AssertionError("unreachable");
        }

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
                + "ns/park enabled=" + enabledMed + "ns/park (nanoTime: "
                + nanoCost + "ns)");
        assertTrue(enabledMed < ENABLED_BUDGET_NANOCOSTS * nanoCost,
                "JFR enabled park-hook cost blew the budget: " + enabledMed
                + "ns/park vs " + (ENABLED_BUDGET_NANOCOSTS * nanoCost) + "ns");
        assertTrue(enabledMed - disabledMed < DELTA_BUDGET_NANOCOSTS * nanoCost,
                "JFR enabled-vs-disabled delta blew the budget: "
                + (enabledMed - disabledMed) + "ns/park vs "
                + (DELTA_BUDGET_NANOCOSTS * nanoCost) + "ns");
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }
}
