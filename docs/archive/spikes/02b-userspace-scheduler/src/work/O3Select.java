package work;

import usched.*;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * O3 — select fairness: 8 pre-filled buffered channels, 20k selectRecv;
 * per-channel counts must be near-uniform (max/min <= 1.3).
 */
public final class O3Select {
    public static void main(String[] args) throws Exception {
        int nCh = 8, nSel = 20_000, prefill = 512;
        GoTimer.start();
        Thread watchdog = Thread.ofPlatform().daemon(true).name("o3-watchdog").start(() -> {
            try { Thread.sleep(120_000); } catch (InterruptedException e) { return; }
            System.out.println("O3 WATCHDOG: hang detected RESULT=FAIL");
            System.out.flush();
            System.exit(2);
        });

        List<GoChannel<Long>> chs = new ArrayList<>();
        for (int i = 0; i < nCh; i++) {
            GoChannel<Long> ch = new GoChannel<>(1024);
            for (int k = 0; k < prefill; k++) ch.trySend((long) k);
            chs.add(ch);
        }
        long[] counts = new long[nCh];
        CountDownLatch done = new CountDownLatch(1);
        // keep channels fed: one feeder goroutine tops up in the background
        Thread feeder = Go.go("feeder", () -> {
            int i = 0;
            while (done.getCount() > 0) {
                GoChannel<Long> ch = chs.get(i % nCh);
                ch.trySend((long) i);
                i++;
                if ((i & 255) == 0) Safepoint.poll();
            }
        });
        Thread sel = Go.go("selector", () -> {
            for (int n = 0; n < nSel; n++) {
                GoSelect.SelResult<Long> sr = GoSelect.selectRecv(chs);
                counts[sr.index()]++;
            }
            done.countDown();
        });
        sel.join(100_000);
        if (sel.isAlive()) {
            System.out.println("o3 selector did not finish RESULT=FAIL");
            System.exit(2);
        }
        feeder.join(10_000);
        watchdog.interrupt();
        long min = Arrays.stream(counts).min().orElse(-1);
        long max = Arrays.stream(counts).max().orElse(-1);
        double ratio = (double) max / Math.max(1, min);
        boolean ok = ratio <= 1.3;
        System.out.printf("o3 nCh=%d nSel=%d counts=%s maxmin_ratio=%.3f RESULT=%s%n",
                nCh, nSel, Arrays.toString(counts), ratio, ok ? "PASS" : "FAIL");
        System.out.println(UserScheduler.instance().statsLine());
        if (!ok) System.exit(2);
        System.out.println("O3 RESULT=PASS");
        System.exit(0);
    }
}
