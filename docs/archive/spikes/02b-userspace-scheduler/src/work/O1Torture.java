package work;

import usched.*;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * O1 — preemption torture on the user-space scheduler (THE falsifying
 * experiment for spike 2b).
 *
 * nSpin CPU-bound spinners (back-edge Safepoint.poll() -> quantum park on
 * the user scheduler) + one latency pair ping-ponging on unbuffered
 * channels. Binary gate: the pair must obtain its samples (near-zero =
 * FAIL). RTT absolute values are NOT expected to match gc-compiled Go
 * (Java prototype + scheduler hops); the load-bearing claim is
 * no-starvation at high throughput.
 *
 * Shutdown: stop=true, brief settle, read continuously-published counters,
 * System.exit (vthreads are daemon; parked ones stay parked).
 *
 * Args: nSpin secs
 * Props: -Dusched.gomaxprocs=N -Dusched.quantumNs=... -Djdk.virtualThreadScheduler.parallelism=...
 */
public final class O1Torture {
    static volatile boolean stop = false;

    public static void main(String[] args) throws Exception {
        int nSpin = Integer.parseInt(args[0]);
        int secs = Integer.parseInt(args[1]);
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();

        long[] counts = new long[nSpin];
        for (int i = 0; i < nSpin; i++) {
            final int idx = i;
            Go.go("spinner-" + idx, () -> {
                long c = 0, sink = 0;
                while (!stop) {
                    sink += c * 0x9E3779B97F4A7C15L;
                    sink ^= sink >>> 29;
                    c++;
                    Safepoint.poll();
                }
                counts[idx] = c;
                Blackhole.consume(sink);
            });
        }

        GoChannel<Long> ping = new GoChannel<>();
        GoChannel<Long> pong = new GoChannel<>();
        GoChannel<Long> done = new GoChannel<>();
        int maxSamples = 200_000;
        long[] rtts = new long[maxSamples];
        AtomicInteger nSamples = new AtomicInteger(0);
        AtomicInteger pongerIters = new AtomicInteger(0);
        Go.go("pinger", () -> {
            int n = 0;
            try {
                while (!stop && n < maxSamples) {
                    long t0 = System.nanoTime();
                    ping.send(t0);
                    pong.recv();
                    rtts[n] = System.nanoTime() - t0;
                    n++;
                    nSamples.set(n);
                }
            } catch (GoPanic gp) { /* shutting down */ }
        });
        Go.go("ponger", () -> {
            try {
                int s = 0;
                for (;;) {
                    GoSelect.SelResult<Long> sr = GoSelect.selectRecv(List.of(ping, done));
                    if (sr.index() == 1) break;
                    pong.send(sr.value());
                    s++;
                    if ((s & 1023) == 0) pongerIters.set(s);
                }
                pongerIters.set(-1); // clean exit marker
            } catch (GoPanic gp) { /* shutting down */ }
        });

        Thread.sleep(secs * 1000L);
        stop = true;
        Thread.sleep(400); // let spinners observe stop and exit
        int n = nSamples.get();

        System.out.printf("o1 nSpin=%d secs=%d cap=%d quantumNs=%d samples=%d%n",
                nSpin, secs, us.cap(), Safepoint.quantumNanos, n);
        System.out.printf("ponger_iters=%d%n", pongerIters.get());
        System.out.printf("spinner_counts=%s%n", Arrays.toString(counts));
        System.out.printf("spinner_cv=%.4f%n", cv(counts));
        if (n == 0) {
            System.out.println("rtt_ns: NO SAMPLES (latency goroutines starved) RESULT=FAIL");
        } else {
            Arrays.sort(rtts, 0, n);
            System.out.printf("rtt_ns mean=%.0f p50=%.0f p99=%.0f max=%.0f%n",
                    mean(rtts, n), pct(rtts, n, 50), pct(rtts, n, 99), (double) rtts[n - 1]);
            System.out.printf("RESULT=%s%n", n >= maxSamples ? "PASS" : "PARTIAL");
        }
        System.out.println(us.statsLine());
        System.out.flush();
        System.exit(0);
    }

    static double mean(long[] a, int n) {
        double s = 0; for (int i = 0; i < n; i++) s += a[i]; return s / n;
    }
    static double pct(long[] sorted, int n, double p) {
        return sorted[(int) Math.min(n - 1, (p / 100.0) * n)];
    }
    static double cv(long[] a) {
        double m = 0; for (long v : a) m += v; m /= a.length;
        if (m == 0) return Double.NaN;
        double sd = 0; for (long v : a) sd += (v - m) * (v - m);
        return Math.sqrt(sd / a.length) / m;
    }
}
