package work;

import loomsim.*;

/**
 * O2 — channel send/recv fairness. 8 senders x 20k messages on one shared
 * channel; single receiver counts per sender. Fairness: all senders get
 * ~equal service (Go pairs unbuffered waiters FIFO; buffered channels
 * likewise). Reports per-sender counts and max/min ratio.
 * Variants: unbuffered (cap 0) and buffered (cap 16).
 */
public final class ChanFairness {
    public static void main(String[] args) throws Exception {
        int cap = Integer.parseInt(args[0]);
        int nSenders = 8, perSender = 20_000;
        Safepoint.mode = Safepoint.Mode.POLL;
        Watchdog.start();

        GoChannel<Integer> ch = new GoChannel<>(cap);
        long[] got = new long[nSenders];
        Thread[] senders = new Thread[nSenders];
        for (int i = 0; i < nSenders; i++) {
            final int id = i;
            senders[i] = Go.go(() -> {
                for (int m = 0; m < perSender; m++) ch.send(id);
            });
        }
        Thread recver = Go.go(() -> {
            for (int m = 0; m < nSenders * perSender; m++) {
                Integer id = ch.recv();
                got[id]++;
            }
        });
        for (Thread t : senders) t.join();
        recver.join();

        long mn = Long.MAX_VALUE, mx = 0;
        for (long v : got) { mn = Math.min(mn, v); mx = Math.max(mx, v); }
        System.out.printf("cap=%d senders=%d perSender=%d%n", cap, nSenders, perSender);
        System.out.printf("per_sender=%s max_min_ratio=%.4f%n",
                java.util.Arrays.toString(got), (double) mx / mn);
        Watchdog.stop();
    }
}
