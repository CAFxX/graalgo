package usched;

import java.util.Arrays;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Segment latency probe for the park->resume path (diagnostic).
 * Enabled with -Dusched.trace=true. Records per-park:
 *   parking->unpark (drainer path) and unpark->resumed (FJP path).
 * Full sample arrays; print with LatencyProbe.report().
 */
public final class LatencyProbe {
    public static final boolean ENABLED = Boolean.getBoolean("usched.trace");

    private static final int N = 1 << 20;
    private static final long[] parkT = new long[N];
    private static final long[] takeT = new long[N];
    private static final long[] unparkT = new long[N];
    private static final long[] resumeT = new long[N];
    // per-park slot: index by a global sequence
    private static final AtomicInteger seq = new AtomicInteger(0);
    private static final AtomicLong lost = new AtomicLong();

    static void parking(Goroutine g) {
        int s = seq.getAndIncrement() & (N - 1);
        g.probeSlot = s;
        parkT[s] = System.nanoTime();
        takeT[s] = 0;
        unparkT[s] = 0;
        resumeT[s] = 0;
    }

    static void unparking(Goroutine g) {
        int s = g.probeSlot;
        if (s >= 0) unparkT[s] = System.nanoTime();
    }

    static void drainerTook(Goroutine g) {
        if (g == null) return;
        int s = g.probeSlot;
        if (s >= 0) takeT[s] = System.nanoTime();
    }

    static void resumed(Goroutine g) {
        int s = g.probeSlot;
        if (s >= 0) resumeT[s] = System.nanoTime();
    }

    public static void report() {
        int n = Math.min(seq.get(), N);
        long[] d = new long[n];
        long[] f = new long[n];
        long[] q = new long[n];
        int nd = 0, nf = 0, nq = 0;
        for (int i = 0; i < n; i++) {
            if (parkT[i] != 0 && takeT[i] != 0) q[nq++] = takeT[i] - parkT[i];
            if (parkT[i] != 0 && unparkT[i] != 0) d[nd++] = unparkT[i] - parkT[i];
            if (unparkT[i] != 0 && resumeT[i] != 0) f[nf++] = resumeT[i] - unparkT[i];
            else if (unparkT[i] != 0) lost.incrementAndGet();
        }
        Arrays.sort(d, 0, nd);
        Arrays.sort(f, 0, nf);
        Arrays.sort(q, 0, nq);
        System.out.printf("trace: queue_wake(parking->drainerTake) n=%d p50=%.0f p99=%.0f max=%.0f ns%n",
                nq, pct(q, nq, 50), pct(q, nq, 99), nq > 0 ? (double) q[nq - 1] : 0);
        System.out.printf("trace: drainer_path(parking->unpark) n=%d p50=%.0f p99=%.0f max=%.0f ns%n",
                nd, pct(d, nd, 50), pct(d, nd, 99), nd > 0 ? (double) d[nd - 1] : 0);
        System.out.printf("trace: fjp_path(unpark->resumed) n=%d p50=%.0f p99=%.0f max=%.0f ns%n",
                nf, pct(f, nf, 50), pct(f, nf, 99), nf > 0 ? (double) f[nf - 1] : 0);
        System.out.printf("trace: lost=%d%n", lost.get());
    }

    private static double pct(long[] s, int n, double p) {
        if (n == 0) return 0;
        return s[(int) Math.min(n - 1, (p / 100.0) * n)];
    }

    private LatencyProbe() {}
}
