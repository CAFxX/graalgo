package work;

import usched.*;
import java.util.Arrays;

/**
 * O4 — timer latency under spinner load: 2 spinners + 2000 x sleep(2 ms).
 * Reports sleep-excess distribution; gate is no-hang + sane p99.
 */
public final class O4Timer {
    static volatile boolean stop = false;

    public static void main(String[] args) throws Exception {
        int nSpin = 2, nSleeps = 2000;
        long sleepNs = 2_000_000L;
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();

        for (int i = 0; i < nSpin; i++) {
            Go.go("spinner-" + i, () -> {
                long sink = 0, c = 0;
                while (!stop) { sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++; Safepoint.poll(); }
                Blackhole.consume(sink);
            });
        }
        long[] excess = new long[nSleeps];
        Thread sleeper = Go.go("sleeper", () -> {
            for (int i = 0; i < nSleeps; i++) {
                long deadline = System.nanoTime() + sleepNs;
                GoTimer.sleep(sleepNs);
                excess[i] = System.nanoTime() - deadline; // >= 0 modulo clock granularity
            }
        });
        sleeper.join(120_000);
        stop = true;
        if (sleeper.isAlive()) {
            System.out.println("o4 sleeper did not finish RESULT=FAIL");
            System.exit(2);
        }
        Arrays.sort(excess);
        double p50 = excess[(int) (0.50 * nSleeps)];
        double p99 = excess[(int) (0.99 * nSleeps)];
        boolean ok = p99 < 50_000_000; // 50 ms sanity bound under load
        System.out.printf("o4 nSpin=%d nSleeps=%d sleepNs=%d excess_ns p50=%.0f p99=%.0f max=%d RESULT=%s%n",
                nSpin, nSleeps, sleepNs, p50, p99, excess[nSleeps - 1], ok ? "PASS" : "FAIL");
        System.out.println(us.statsLine());
        if (!ok) System.exit(2);
        System.out.println("O4 RESULT=PASS");
        System.exit(0);
    }
}
