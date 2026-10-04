package work;

import usched.*;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

/**
 * O5 — GOMAXPROCS scaling shape: 16 CPU-bound goroutines, fixed 3 s window,
 * total work units at cap=1 vs cap=2 (separate runs). Expect ~2x.
 */
public final class O5Scale {
    public static void main(String[] args) throws Exception {
        int nG = 16, secs = 3;
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();
        AtomicLong total = new AtomicLong(0);
        CountDownLatch started = new CountDownLatch(nG);
        CountDownLatch done = new CountDownLatch(nG);
        for (int i = 0; i < nG; i++) {
            Go.go("worker-" + i, () -> {
                started.countDown();
                // spin barrier (CountDownLatch.await() would bypass the scheduler)
                while (started.getCount() > 0) Safepoint.poll();
                long c = 0, sink = 0;
                long end = System.nanoTime() + secs * 1_000_000_000L;
                while (System.nanoTime() < end) {
                    sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++;
                    Safepoint.poll();
                }
                total.addAndGet(c);
                Blackhole.consume(sink);
                done.countDown();
            });
        }
        if (!done.await(60, TimeUnit.SECONDS)) {
            System.out.println("o5 workers did not finish RESULT=FAIL");
            System.exit(2);
        }
        System.out.printf("o5 nG=%d secs=%d cap=%d total_units=%d units_per_sec=%.1f%n",
                nG, secs, us.cap(), total.get(), total.get() / (double) secs);
        System.out.println(us.statsLine());
        System.out.println("O5 RESULT=PASS");
        System.exit(0);
    }
}
