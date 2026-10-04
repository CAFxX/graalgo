package work;

import java.util.Arrays;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Minimal repro: vthread offers timestamp to LBQ; platform drainer takes it
 * and records offer->take latency. No FJP resume, no scheduler logic.
 */
public final class LbqRepro {
    public static void main(String[] args) throws Exception {
        int n = 30000;
        LinkedBlockingQueue<Long> q = new LinkedBlockingQueue<>();
        long[] costs = new long[n];
        AtomicLong idx = new AtomicLong(0);
        Thread drainer = Thread.ofPlatform().daemon(true).start(() -> {
            try {
                for (;;) {
                    long t0 = q.take();
                    long i = idx.getAndIncrement();
                    if (i < n) costs[(int) i] = System.nanoTime() - t0;
                    else break;
                }
            } catch (InterruptedException e) {}
        });
        Thread.sleep(300);
        // warmup
        for (int i = 0; i < 3000; i++) q.offer(System.nanoTime());
        Thread.sleep(100);
        idx.set(0);
        Thread v = Thread.ofVirtual().start(() -> {
            for (int i = 0; i < n; i++) {
                q.offer(System.nanoTime());
                // pace like the scheduler: do ~20us of "work" then offer again
                long end = System.nanoTime() + 20_000;
                while (System.nanoTime() < end) { /* busy */ }
            }
        });
        v.join(60000);
        Thread.sleep(200);
        Arrays.sort(costs, 0, n);
        System.out.printf("lbq vthread-offer -> platform-take ns p50=%.0f p99=%.0f max=%d%n",
                (double) costs[n/2], (double) costs[(int)(n*0.99)], costs[n-1]);
        System.exit(0);
    }
}
