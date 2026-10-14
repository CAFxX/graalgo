package work;

import loomsim.*;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicLong;

/**
 * O5 — GOMAXPROCS throughput scaling shape. 16 workers burn a fixed digest;
 * aggregate throughput measured at carrier parallelism 1, 2 (separate JVM
 * launches via -Djdk.virtualThreadScheduler.parallelism=N — the launcher's
 * GOMAXPROCS translation). Expect near-linear scaling to #cores, then
 * plateau; oversubscription (4 on 2 cores) must not collapse.
 */
public final class GomaxprocsScale {
    public static void main(String[] args) throws Exception {
        int secs = Integer.parseInt(args[0]);
        Safepoint.mode = Safepoint.Mode.POLL;
        Watchdog.start();

        int workers = 16;
        AtomicLong total = new AtomicLong();
        CountDownLatch done = new CountDownLatch(workers);
        long deadline = System.nanoTime() + secs * 1_000_000_000L;
        for (int i = 0; i < workers; i++) {
            Go.go(() -> {
                long local = 0, x = 0x12345678L;
                while (System.nanoTime() < deadline) {
                    for (int k = 0; k < 1000; k++) { // inner batch: one safepoint per 1k ops
                        x ^= x << 13; x ^= x >>> 7; x ^= x << 17;
                        local++;
                    }
                    Safepoint.poll(); // back-edge of the outer loop
                }
                total.addAndGet(local);
                Blackhole.consume(x);
                done.countDown();
            });
        }
        done.await();
        double ops = total.get();
        System.out.printf("parallelism=%d workers=%d secs=%d mops=%.1f mops_per_sec=%.1f%n",
                Go.carrierParallelism(), workers, secs, ops / 1e6, ops / 1e6 / secs);
        Watchdog.stop();
    }
}
