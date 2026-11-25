package work;

import loomsim.*;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * O1 — preemption torture (THE falsifying experiment for the Loom gap).
 *
 * nSpin CPU-bound goroutines spin in a tight loop with a back-edge
 * Safepoint.poll() (what the Truffle AST would emit), while one latency pair
 * ping-pongs on unbuffered channels. Measures:
 *   - per-spinner iteration counts -> fairness (CV across spinners)
 *   - ping-pong RTT distribution (p50/p99/max) -> latency collapse detector
 *   - sample count published CONTINUOUSLY (AtomicInteger) so a pair that is
 *     still parked at shutdown is not misreported as starved
 *
 * Modes: OFF (control: Loom's native behavior), UNCOND (yield every
 * back-edge), POLL (yield only on watchdog pulse).
 *
 * Run with -Djdk.virtualThreadScheduler.parallelism=2 (the GOMAXPROCS
 * translation). Shutdown: stop=true, brief settle, read counters,
 * System.exit (virtual threads are daemon; no join needed).
 */
public final class PreemptTorture {
    static volatile boolean stop = false;

    public static void main(String[] args) throws Exception {
        Safepoint.Mode mode = Safepoint.Mode.valueOf(args[0]); // OFF|UNCOND|POLL
        int nSpin = Integer.parseInt(args[1]);
        int secs = Integer.parseInt(args[2]);
        Safepoint.mode = mode;
        if (mode == Safepoint.Mode.POLL) Watchdog.start();
        GoTimer.start();

        long[] counts = new long[nSpin];
        Thread[] spinners = new Thread[nSpin];
        for (int i = 0; i < nSpin; i++) {
            final int idx = i;
            spinners[i] = Go.go(() -> {
                long c = 0;
                long sink = 0;
                while (!stop) {
                    // small amount of real work so the loop isn't degenerate
                    sink += c * 0x9E3779B97F4A7C15L;
                    sink ^= sink >>> 29;
                    c++;
                    Safepoint.poll(); // <-- the AST back-edge safepoint
                }
                counts[idx] = c;
                Blackhole.consume(sink);
            });
        }

        // latency pair: ping-pong RTT samples, count published continuously
        GoChannel<Long> ping = new GoChannel<>();
        GoChannel<Long> pong = new GoChannel<>();
        int maxSamples = 1_000_000;
        long[] rtts = new long[maxSamples];
        AtomicInteger nSamples = new AtomicInteger(0);
        AtomicInteger pingerStarted = new AtomicInteger(0);
        AtomicInteger pongerStarted = new AtomicInteger(0);
        AtomicInteger pongerIters = new AtomicInteger(0);
        Go.go(() -> {
            pingerStarted.set(1);
            int n = 0;
            while (!stop && n < maxSamples) {
                long t0 = System.nanoTime();
                ping.send(t0);
                pong.recv();
                long t1 = System.nanoTime();
                rtts[n] = t1 - t0;
                n = n + 1;
                nSamples.set(n);
            }
        });
        Go.go(() -> {
            pongerStarted.set(1);
            int s = 0;
            while (!stop) {
                Long t = ping.recv();
                if (t == null) break; // closed channel: harness shutting down
                pong.send(t);
                s++;
                if ((s & 1023) == 0) pongerIters.set(s);
            }
            pongerIters.set(s);
        });

        Thread.sleep(secs * 1000L);
        stop = true;
        for (Thread t : spinners) t.join(5000); // spinners spin on !stop: exit promptly; join gives HB
        Thread.sleep(300); // let the latency pair settle; parked threads stay parked (daemon)
        int n = nSamples.get();

        System.out.printf("mode=%s nSpin=%d secs=%d carriers=%d samples=%d%n",
                mode, nSpin, secs, Go.carrierParallelism(), n);
        System.out.printf("pinger_started=%d ponger_started=%d ponger_iters=%d%n",
                pingerStarted.get(), pongerStarted.get(), pongerIters.get());
        System.out.printf("watchdog_pulses=%d yields_honored=%d%n",
                Watchdog.pulses.get(), Safepoint.yieldsHonored.get());
        System.out.printf("spinner_counts=%s%n", java.util.Arrays.toString(counts));
        System.out.printf("spinner_cv=%.4f%n", cv(counts));
        if (n == 0) {
            System.out.println("rtt_ns: NO SAMPLES (latency goroutines starved)");
        } else {
            java.util.Arrays.sort(rtts, 0, n);
            double mean = mean(rtts, n);
            System.out.printf("rtt_ns mean=%.0f p50=%.0f p99=%.0f max=%.0f%n",
                    mean, pct(rtts, n, 50), pct(rtts, n, 99), (double) rtts[n - 1]);
        }
        Watchdog.stop();
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
        double sd = 0; for (long v : a) sd += (v - m) * (v - m); sd = Math.sqrt(sd / a.length);
        return m == 0 ? Double.NaN : sd / m;
    }
}
