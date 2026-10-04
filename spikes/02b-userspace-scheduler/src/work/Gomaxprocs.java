package work;

import usched.*;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

/**
 * GOMAXPROCS fidelity (falsifier 5).
 *  (a) observed true parallelism never exceeds the cap (hard assert in the
 *      drainer would already have thrown; this reports maxTrueParallel).
 *  (b) mixed I/O+compute: 2 spinners + 1 sleeper (Loom sleep -> carrier
 *      unmount, invisible to the scheduler) at cap=2; measures spinner
 *      throughput vs the pure-compute baseline to QUANTIFY the known
 *      under-utilization subtlety (bounded, not unbounded).
 *
 * Args: mode  (par | mixed)
 * Daemon-exit shutdown.
 */
public final class Gomaxprocs {
    static volatile boolean stop = false;

    public static void main(String[] args) throws Exception {
        String mode = args[0];
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();
        if (mode.equals("par")) {
            int nSpin = 8, secs = 4;
            CountDownLatch done = new CountDownLatch(nSpin);
            for (int i = 0; i < nSpin; i++) {
                Go.go("spinner-" + i, () -> {
                    long sink = 0, c = 0;
                    long end = System.nanoTime() + secs * 1_000_000_000L;
                    while (System.nanoTime() < end) {
                        sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++;
                        Safepoint.poll();
                    }
                    Blackhole.consume(sink);
                    done.countDown();
                });
            }
            done.await(60, TimeUnit.SECONDS);
            System.out.printf("gomaxprocs_par cap=%d maxTrueParallel=%d RESULT=%s%n",
                    us.cap(), us.maxTrueParallel.get(),
                    us.maxTrueParallel.get() <= us.cap() ? "PASS" : "FAIL");
        } else if (mode.equals("mixed") || mode.equals("mixed_nosleeper")) {
            int secs = 6;
            AtomicLong spinIters = new AtomicLong(0);
            for (int i = 0; i < 2; i++) {
                Go.go("spinner-" + i, () -> {
                    long sink = 0, c = 0;
                    while (!stop) { sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++; Safepoint.poll(); }
                    spinIters.addAndGet(c);
                    Blackhole.consume(sink);
                });
            }
            if (mode.equals("mixed")) {
                Go.go("sleeper", () -> {
                    while (!stop) {
                        try { Thread.sleep(50); } catch (InterruptedException e) { break; }
                    }
                });
            }
            Thread.sleep(secs * 1000L);
            stop = true;
            Thread.sleep(300);
            System.out.printf("gomaxprocs_mixed cap=%d spinner_iters=%d iters_per_sec=%.1f%n",
                    us.cap(), spinIters.get(), spinIters.get() / (double) secs);
            System.out.printf("NOTE: report alongside the pure-compute baseline (2 spinners, no sleeper)%n");
        }
        System.out.println(us.statsLine());
        System.out.flush();
        System.exit(0);
    }
}
