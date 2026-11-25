package work;

import usched.*;
import java.util.Arrays;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Quiescence probe (Carlo's scenario): 10,000 spinners. Verifies:
 *  (a) the system repeatedly reaches the all-parked state
 *      (maxParked ~= N - cap via minInflightObserved),
 *  (b) quanta are distributed fairly: per-spinner quanta max/min <= 2x mean,
 *  (c) the FJP never floods (maxUnparkedNotResumed small),
 *  (d) the drainer is not a bottleneck (utilization << 100%).
 *  (e) every spinner is admitted at least once BEFORE stop (firstAdmissions)
 *      — the true starvation signal.
 *
 * Short quantum (100 us) for statistical power: 20 s -> ~400k quanta over
 * 10k spinners, mean ~40.
 *
 * Drain discipline (post-2026-10-03 triage): the old fixed 800 ms sleep
 * after stop produced thousands of quanta[idx]==0 readings that were NOT
 * scheduler starvation — those spinners had been admitted ~7x during the
 * run but were still parked in runFifo when the main thread read the
 * array (drain-window artifact; see REPORT.md). The harness now drains via
 * a latch (every spinner counts down after recording its quanta) and
 * samples firstAdmissions BEFORE setting stop, so "never admitted during
 * the run" is measured directly instead of inferred from a racy window.
 * -Dquiescence.drainMs=N restores the old fixed-window behavior for
 * artifact-demonstration runs (diagnostic only, not the battery).
 *
 * Args: nSpin secs  (quantum via -Dusched.quantumNs)
 * Daemon-exit shutdown like O1.
 */
public final class Quiescence {
    static volatile boolean stop = false;

    public static void main(String[] args) throws Exception {
        int nSpin = Integer.parseInt(args[0]);
        int secs = Integer.parseInt(args[1]);
        long fixedDrainMs = Long.getLong("quiescence.drainMs", -1);
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();

        long[] quanta = new long[nSpin];
        CountDownLatch drained = new CountDownLatch(nSpin);
        for (int i = 0; i < nSpin; i++) {
            final int idx = i;
            Go.go("spinner-" + idx, () -> {
                long sink = 0, c = 0;
                while (!stop) { sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++; Safepoint.poll(); }
                quanta[idx] = Goroutine.current().quanta;
                Blackhole.consume(sink);
                drained.countDown();
            });
        }
        Thread.sleep(secs * 1000L);
        // Sample BEFORE stop: distinct goroutines admitted at least once
        // during the run. This is the starvation signal, free of any
        // post-stop drain race.
        long admittedBeforeStop = us.firstAdmissions.sum();
        stop = true;
        long drainStart = System.nanoTime();
        boolean drainedOk;
        long drainWaitMs;
        if (fixedDrainMs >= 0) {
            Thread.sleep(fixedDrainMs);
            drainedOk = drained.getCount() == 0;
            drainWaitMs = fixedDrainMs;
        } else {
            drainedOk = drained.await(120, TimeUnit.SECONDS);
            drainWaitMs = (System.nanoTime() - drainStart) / 1_000_000;
        }

        long[] q = Arrays.copyOf(quanta, nSpin);
        Arrays.sort(q);
        // exclude spinners that never completed a quantum
        int first = 0;
        while (first < nSpin && q[first] == 0) first++;
        double mean = 0;
        for (int i = first; i < nSpin; i++) mean += q[i];
        mean /= Math.max(1, nSpin - first);
        long min = first < nSpin ? q[first] : 0;
        long max = q[nSpin - 1];
        double ratio = mean > 0 ? max / mean : Double.NaN;
        boolean fair = admittedBeforeStop == nSpin && drainedOk && ratio <= 2.0;

        long maxParked = nSpin - us.minInflightObserved;
        System.out.printf("quiescence nSpin=%d secs=%d cap=%d quantumNs=%d%n",
                nSpin, secs, us.cap(), Safepoint.quantumNanos);
        System.out.printf("quanta_per_spinner min=%d max=%d mean=%.1f max_over_mean=%.2f never_scheduled=%d FAIR=%s%n",
                min, max, mean, ratio, first, fair ? "PASS" : "FAIL");
        System.out.printf("admitted_before_stop=%d (nSpin=%d) drain_wait_ms=%d drained_all=%b fixedDrainMs=%d%n",
                admittedBeforeStop, nSpin, drainWaitMs, drainedOk, fixedDrainMs);
        System.out.printf("maxParkedObserved=%d (N=%d, cap=%d)%n", maxParked, nSpin, us.cap());
        System.out.println(us.statsLine());
        System.out.printf("RESULT=%s%n", fair ? "PASS" : "FAIL");
        System.out.flush();
        System.exit(fair ? 0 : 2);
    }
}
