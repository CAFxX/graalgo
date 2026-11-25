package work;

import loomsim.*;

/**
 * O4 — timer latency distribution. A goroutine loops: sleep(10ms) via
 * GoTimer, measuring actual wake delay vs requested. Runs under load
 * (nSpin spinners with back-edge safepoints, POLL mode). Reports excess
 * latency p50/p99/max in microseconds.
 */
public final class TimerLatency {
    static volatile boolean stop = false;

    public static void main(String[] args) throws Exception {
        int nSpin = Integer.parseInt(args[0]);
        int samples = Integer.parseInt(args[1]);
        Safepoint.mode = Safepoint.Mode.POLL;
        Watchdog.start();
        GoTimer.start();

        for (int i = 0; i < nSpin; i++) {
            Go.go(() -> {
                long sink = 0, c = 0;
                while (!stop) { sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++; Safepoint.poll(); }
                Blackhole.consume(sink);
            });
        }

        long[] excess = new long[samples]; // micros over the requested 10ms
        for (int i = 0; i < samples; i++) {
            long t0 = System.nanoTime();
            GoTimer.sleep(10_000_000L);
            long dt = System.nanoTime() - t0;
            excess[i] = (dt - 10_000_000L) / 1000L;
        }
        stop = true;
        java.util.Arrays.sort(excess);
        System.out.printf("nSpin=%d samples=%d carriers=%d%n", nSpin, samples, Go.carrierParallelism());
        System.out.printf("excess_us p50=%d p99=%d max=%d%n",
                pct(excess, 50), pct(excess, 99), excess[excess.length - 1]);
        Watchdog.stop();
    }

    static long pct(long[] s, double p) { return s[(int) Math.min(s.length - 1, p / 100.0 * s.length)]; }
}
