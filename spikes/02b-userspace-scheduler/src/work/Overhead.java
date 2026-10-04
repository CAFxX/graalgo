package work;

import usched.*;
import java.util.Arrays;
import java.util.concurrent.CountDownLatch;

/**
 * Overhead budget (falsifier 4): single goroutine, no contention, so
 * park->resume latency is pure scheduler overhead (no queueing):
 *   park/unmount + enqueue + drainer wakeup + unpark + FJP submit + pickup.
 * Budget: mean < 10 us (<1% at 1 ms quanta); < 5 us for the 100 us-quantum
 * case (<5%). Also measures the back-edge poll() fast path cost.
 */
public final class Overhead {
    public static void main(String[] args) throws Exception {
        int iters = Integer.parseInt(args[0]);
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();
        CountDownLatch done = new CountDownLatch(1);
        long[] costs = new long[iters];

        Thread t = Go.go("probe", () -> {
            Goroutine g = Goroutine.current();
            // warmup
            for (int i = 0; i < 20_000; i++) us.quantumPark(g);
            for (int i = 0; i < iters; i++) {
                long t0 = System.nanoTime();
                us.quantumPark(g);
                costs[i] = System.nanoTime() - t0;
            }
            done.countDown();
        });
        done.await();
        t.join(10_000);

        Arrays.sort(costs);
        double mean = 0;
        for (long c : costs) mean += c;
        mean /= iters;
        double p50 = costs[(int) (0.50 * iters)];
        double p99 = costs[(int) (0.99 * iters)];
        System.out.printf("overhead iters=%d park_resume_ns mean=%.0f p50=%.0f p99=%.0f max=%d%n",
                iters, mean, p50, p99, costs[iters - 1]);
        System.out.printf("budget_1ms_quantum: mean_ns=%.0f < 10000 (1%%) -> %s%n",
                mean, mean < 10_000 ? "PASS" : "FAIL");
        System.out.printf("budget_100us_quantum: mean_ns=%.0f < 5000 (5%%) -> %s%n",
                mean, mean < 5_000 ? "PASS" : "FAIL");
        // Budget against the CONFIGURED quantum: the per-hop cost is
        // quantum-independent; what changes is the budget it is measured
        // against. RESULT follows the configured quantum so each battery
        // leg (1ms, 10ms) reports its own verdict honestly.
        long qNs = Safepoint.quantumNanos;
        long budgetNs = qNs / 100; // 1%
        System.out.printf("budget_configured_quantum: mean_ns=%.0f < %d (1%% of quantumNs=%d) -> %s%n",
                mean, budgetNs, qNs, mean < budgetNs ? "PASS" : "FAIL");

        // back-edge poll() fast path (quantum never expires)
        long qp = Safepoint.quantumNanos;
        int polls = 10_000_000;
        long t0 = System.nanoTime();
        for (int i = 0; i < polls; i++) Safepoint.poll();
        double pollNs = (System.nanoTime() - t0) / (double) polls;
        System.out.printf("poll_fastpath_ns_per_poll=%.2f (quantumNs=%d, main thread: no goroutine -> early exit)%n",
                pollNs, qp);
        // NOTE: on the main thread Goroutine.current() is null -> early exit;
        // measure on a goroutine for the real cost:
        CountDownLatch done2 = new CountDownLatch(1);
        final double[] pollNsG = new double[1];
        Go.go("pollprobe", () -> {
            long a = System.nanoTime();
            for (int i = 0; i < polls; i++) Safepoint.poll();
            pollNsG[0] = (System.nanoTime() - a) / (double) polls;
            done2.countDown();
        });
        done2.await();
        System.out.printf("poll_fastpath_ns_per_poll_on_goroutine=%.2f%n", pollNsG[0]);
        System.out.println(us.statsLine());
        boolean ok = mean < budgetNs;
        System.out.printf("RESULT=%s%n", ok ? "PASS" : "FAIL");
        System.exit(ok ? 0 : 2);
    }
}
