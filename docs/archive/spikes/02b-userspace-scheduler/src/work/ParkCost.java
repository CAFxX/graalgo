package work;

import java.util.Arrays;
import java.util.concurrent.locks.LockSupport;

/**
 * Measures vthread park/unmount latency: time from LockSupport.park() call
 * to the carrier being free (approximated by another thread observing).
 * Actually simpler: measure park->unpark roundtrip where unpark comes from
 * another thread immediately.
 */
public final class ParkCost {
    public static void main(String[] args) throws Exception {
        int n = 20000;
        long[] costs = new long[n];
        Thread[] holder = new Thread[1];
        for (int i = 0; i < 3000; i++) { // warmup
            Thread v = Thread.ofVirtual().start(() -> LockSupport.park());
            while (v.getState() != Thread.State.WAITING) Thread.onSpinWait();
            LockSupport.unpark(v);
            v.join(1000);
        }
        for (int i = 0; i < n; i++) {
            Thread v = Thread.ofVirtual().start(() -> LockSupport.park());
            long t0 = System.nanoTime();
            // wait until parked (unmounted)
            while (v.getState() != Thread.State.WAITING) Thread.onSpinWait();
            long t1 = System.nanoTime();
            LockSupport.unpark(v);
            v.join(1000);
            costs[i] = t1 - t0;
        }
        Arrays.sort(costs);
        System.out.printf("vthread park->WAITING state ns p50=%.0f p99=%.0f max=%d%n",
                (double) costs[n/2], (double) costs[(int)(n*0.99)], costs[n-1]);
        System.exit(0);
    }
}
