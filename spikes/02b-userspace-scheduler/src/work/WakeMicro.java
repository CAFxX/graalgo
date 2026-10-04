package work;

import java.util.Arrays;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.locks.LockSupport;

/**
 * Isolate wakeup costs: (a) platform thread park/unpark ping-pong,
 * (b) virtual thread park/unpark ping-pong (via FJP).
 * Strict alternation, no lost wakeups: use request/ack counters.
 */
public final class WakeMicro {
    static class Pair {
        final AtomicLong req = new AtomicLong(0);
        final AtomicLong ack = new AtomicLong(0);
    }

    public static void main(String[] args) throws Exception {
        int n = 30000;
        // (a) platform threads
        long[] plat = pingpong(n, false);
        // (b) virtual threads
        long[] virt = pingpong(n, true);
        Arrays.sort(plat); Arrays.sort(virt);
        System.out.printf("platform park/unpark rtt ns p50=%.0f p99=%.0f%n",
                (double) plat[n/2], (double) plat[(int)(n*0.99)]);
        System.out.printf("virtual  park/unpark rtt ns p50=%.0f p99=%.0f%n",
                (double) virt[n/2], (double) virt[(int)(n*0.99)]);
        System.exit(0);
    }

    static long[] pingpong(int n, boolean virtual) throws Exception {
        Pair p = new Pair();
        long[] costs = new long[n];
        CountDownLatch ready = new CountDownLatch(1);
        Thread[] holder = new Thread[1];
        Runnable body = () -> {
            ready.countDown();
            for (int i = 1; i <= 2000 + n; i++) {
                while (p.req.get() < i) LockSupport.park();
                p.ack.set(i);
                LockSupport.unpark(holder[0]);
            }
        };
        Thread peer = virtual ? Thread.ofVirtual().start(body) : Thread.ofPlatform().start(body);
        ready.await();
        Thread me = Thread.currentThread();
        holder[0] = me;
        // warmup
        for (int i = 1; i <= 2000; i++) {
            p.req.set(i); LockSupport.unpark(peer);
            while (p.ack.get() < i) LockSupport.park();
        }
        for (int i = 2001; i <= 2000 + n; i++) {
            long t0 = System.nanoTime();
            p.req.set(i); LockSupport.unpark(peer);
            while (p.ack.get() < i) LockSupport.park();
            costs[i - 2001] = System.nanoTime() - t0;
        }
        peer.join(10000);
        return costs;
    }
}
