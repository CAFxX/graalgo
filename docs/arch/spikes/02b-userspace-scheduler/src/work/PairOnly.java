package work;

import usched.*;
import java.util.Arrays;
import java.util.List;

/**
 * Micro-diagnostic: latency pair ONLY (no spinners). Measures pure
 * scheduler-hop cost: parkIdle -> drainer -> unpark -> FJP -> resume.
 * If RTT is low here but high in O1, the problem is spinner interference
 * (slot contention), not hop cost.
 */
public final class PairOnly {
    public static void main(String[] args) throws Exception {
        int nSamples = Integer.parseInt(args[0]);
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();

        GoChannel<Long> ping = new GoChannel<>();
        GoChannel<Long> pong = new GoChannel<>();
        long[] rtts = new long[nSamples];
        java.util.concurrent.atomic.AtomicInteger n = new java.util.concurrent.atomic.AtomicInteger(0);
        java.util.concurrent.CountDownLatch done = new java.util.concurrent.CountDownLatch(2);

        Go.go("pinger", () -> {
            try {
                for (int i = 0; i < nSamples; i++) {
                    long t0 = System.nanoTime();
                    ping.send(t0);
                    pong.recv();
                    rtts[i] = System.nanoTime() - t0;
                }
                n.set(nSamples);
            } catch (GoPanic gp) {}
            done.countDown();
        });
        Go.go("ponger", () -> {
            try {
                for (int i = 0; i < nSamples; i++) {
                    Long t = ping.recv();
                    pong.send(t);
                }
            } catch (GoPanic gp) {}
            done.countDown();
        });
        done.await();
        Arrays.sort(rtts);
        System.out.printf("paironly n=%d rtt_ns mean=%.0f p50=%.0f p99=%.0f max=%d%n",
                nSamples, mean(rtts), pct(rtts, 50), pct(rtts, 99), rtts[nSamples-1]);
        System.out.println(us.statsLine());
        if (LatencyProbe.ENABLED) LatencyProbe.report();
        System.exit(0);
    }
    static double mean(long[] a) { double s=0; for(long v:a) s+=v; return s/a.length; }
    static double pct(long[] s, double p) { return s[(int)Math.min(s.length-1,(p/100.0)*s.length)]; }
}
