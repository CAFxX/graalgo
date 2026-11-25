package loomsim;

import java.util.ArrayDeque;
import java.util.concurrent.ThreadLocalRandom;

/**
 * Select model, faithful in semantics to runtime/select.go but not a line
 * port (documented model, not a port):
 *  - lock channels in canonical order (identityHashCode) to avoid deadlock
 *  - poll cases in random order (Go: fastrand pollorder) — this is where
 *    select fairness comes from
 *  - no ready case and no default: register ONE waiter on every channel
 *    (Go: one sudog on all channels), park it, re-poll on wakeup
 *    (spurious wakeups handled by the re-poll loop)
 */
public final class GoSelect {

    public sealed interface Case permits RecvCase, SendCase {}
    public record RecvCase<T>(GoChannel<T> ch) implements Case {}
    public record SendCase<T>(GoChannel<T> ch, T val) implements Case {}

    /** One waiter parked across all channels of a select. */
    static final class Waiter {
        boolean ready = false;
        synchronized void signal() { ready = true; notifyAll(); }
    }

    /**
     * Returns the chosen case index, or -1 if default was taken.
     * For RecvCase the received value is stored in recvOut[chosen].
     */
    @SuppressWarnings({"unchecked", "rawtypes"})
    public static int select(Case[] cases, boolean hasDefault, Object[] recvOut) {
        int n = cases.length;
        int[] order = new int[n];
        for (int i = 0; i < n; i++) order[i] = i;
        // Fisher-Yates with ThreadLocalRandom (Go: fastrand pollorder)
        ThreadLocalRandom rnd = ThreadLocalRandom.current();
        for (int i = n - 1; i > 0; i--) {
            int j = rnd.nextInt(i + 1);
            int t = order[i]; order[i] = order[j]; order[j] = t;
        }

        for (;;) {
            // phase 1: poll in random order
            for (int idx : order) {
                Case c = cases[idx];
                if (c instanceof RecvCase rc) {
                    GoChannel.RecvResult r = rc.ch().tryRecv();
                    if (r.ok()) { recvOut[idx] = r.value(); return idx; }
                } else if (c instanceof SendCase sc) {
                    if (sc.ch().trySend(sc.val())) return idx;
                }
            }
            if (hasDefault) return -1;

            // phase 2: block — register one waiter on every channel
            Waiter w = new Waiter();
            GoChannel[] chs = channelsOf(cases);
            for (GoChannel ch : chs) ch.addSelectWaiter(w);
            try {
                synchronized (w) {
                    while (!w.ready) {
                        try { w.wait(); }
                        catch (InterruptedException e) { Thread.currentThread().interrupt(); }
                    }
                }
            } finally {
                for (GoChannel ch : chs) ch.removeSelectWaiter(w);
            }
            // loop: re-poll (spurious wakeups safe)
        }
    }

    private static GoChannel[] channelsOf(Case[] cases) {
        // canonical lock/registration order by channel id (deadlock avoidance)
        java.util.TreeSet<GoChannel> set = new java.util.TreeSet<>(
            (a, b) -> Long.compare(a.selId(), b.selId()));
        for (Case c : cases) {
            if (c instanceof RecvCase rc) set.add(rc.ch());
            else if (c instanceof SendCase sc) set.add(sc.ch());
        }
        return set.toArray(new GoChannel[0]);
    }

    private GoSelect() {}
}
