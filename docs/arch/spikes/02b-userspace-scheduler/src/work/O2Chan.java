package work;

import usched.*;
import java.util.Arrays;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * O2 — channel correctness under the user-space scheduler.
 * 8 senders x 20k msgs, unbuffered then buffered(16); per-sender counts must
 * be exactly equal; global watchdog (120 s) turns any hang into FAIL.
 */
public final class O2Chan {
    public static void main(String[] args) throws Exception {
        int nSend = 8, nMsg = 20_000;
        GoTimer.start();
        Thread watchdog = Thread.ofPlatform().daemon(true).name("o2-watchdog").start(() -> {
            try { Thread.sleep(120_000); } catch (InterruptedException e) { return; }
            System.out.println("O2 WATCHDOG: hang detected RESULT=FAIL");
            System.out.println(UserScheduler.instance().statsLine());
            System.out.flush();
            System.exit(2);
        });

        for (int cap : new int[]{0, 16}) {
            GoChannel<Long> ch = new GoChannel<>(cap);
            long[] counts = new long[nSend];
            CountDownLatch sendersDone = new CountDownLatch(nSend);
            for (int i = 0; i < nSend; i++) {
                final int idx = i;
                Go.go("sender-" + idx, () -> {
                    for (int m = 0; m < nMsg; m++) ch.send((long) idx * 1_000_000L + m);
                    sendersDone.countDown();
                });
            }
            Thread receiver = Go.go("receiver", () -> {
                for (int m = 0; m < nSend * nMsg; m++) {
                    long v = ch.recv();
                    counts[(int) (v / 1_000_000L)]++;
                    if ((m & 4095) == 0) Safepoint.poll();
                }
            });
            if (!sendersDone.await(100, TimeUnit.SECONDS)) {
                System.out.printf("o2 cap=%d senders did not finish RESULT=FAIL%n", cap);
                System.exit(2);
            }
            receiver.join(30_000);
            if (receiver.isAlive()) {
                System.out.printf("o2 cap=%d receiver did not finish RESULT=FAIL%n", cap);
                System.exit(2);
            }
            long min = Arrays.stream(counts).min().orElse(-1);
            long max = Arrays.stream(counts).max().orElse(-1);
            boolean ok = min == nMsg && max == nMsg;
            System.out.printf("o2 cap=%d per_sender_min=%d per_sender_max=%d expected=%d RESULT=%s%n",
                    cap, min, max, nMsg, ok ? "PASS" : "FAIL");
            if (!ok) System.exit(2);
            UserScheduler.instance().awaitQuiescent();
        }
        watchdog.interrupt();
        System.out.println(UserScheduler.instance().statsLine());
        System.out.println("O2 RESULT=PASS");
        System.exit(0);
    }
}
