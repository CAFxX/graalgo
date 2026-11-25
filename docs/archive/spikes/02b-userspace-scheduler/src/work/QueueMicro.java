package work;

import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.locks.LockSupport;
import java.util.Arrays;

/** True ping-pong handoff latency: A offers, B takes, B offers ack, A takes ack. */
public final class QueueMicro {
    public static void main(String[] args) throws Exception {
        int n = 50000;
        LinkedBlockingQueue<Long> q1 = new LinkedBlockingQueue<>();
        LinkedBlockingQueue<Long> q2 = new LinkedBlockingQueue<>();
        long[] costs = new long[n];
        Thread b = Thread.ofPlatform().daemon(true).start(() -> {
            try {
                for (int i = 0; i < n; i++) {
                    q1.take();
                    q2.offer(System.nanoTime());
                }
            } catch (InterruptedException e) {}
        });
        Thread.sleep(300);
        // warmup
        for (int i = 0; i < 5000; i++) { q1.offer(0L); q2.take(); }
        for (int i = 0; i < n; i++) {
            long t0 = System.nanoTime();
            q1.offer(t0);
            long t1 = q2.take();
            costs[i] = t1 - t0; // full round trip: 2 handoffs
        }
        b.join(10000);
        Arrays.sort(costs);
        System.out.printf("lbq pingpong_rtt ns p50=%.0f p99=%.0f max=%d (2 handoffs)%n",
            (double)costs[n/2], (double)costs[(int)(n*0.99)], costs[n-1]);
        // LockSupport park/unpark micro
        Thread cur = Thread.currentThread();
        long[] pu = new long[n];
        Thread c = Thread.ofPlatform().daemon(true).start(() -> {
            for (int i = 0; i < n; i++) {
                LockSupport.park();
                LockSupport.unpark(cur);
            }
        });
        Thread.sleep(200);
        for (int i = 0; i < 5000; i++) { LockSupport.unpark(c); LockSupport.park(); }
        for (int i = 0; i < n; i++) {
            long t0 = System.nanoTime();
            LockSupport.unpark(c);
            LockSupport.park();
            pu[i] = System.nanoTime() - t0;
        }
        Arrays.sort(pu);
        System.out.printf("park_unpark pingpong_rtt ns p50=%.0f p99=%.0f max=%d (2 handoffs)%n",
            (double)pu[n/2], (double)pu[(int)(n*0.99)], pu[n-1]);
        System.exit(0);
    }
}
