package usched;

import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.List;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Simplified select (spike 2b): recv-only multi-channel select with uniform
 * random choice among ready cases (Go semantics for the fairness probe).
 *
 * Parking goes through the armed-ticket protocol: the waiter registers on
 * all channels only after armPark(); the first signaling channel wins the
 * claimed CAS and the wakeup. After wakeup the waiter unregisters and
 * re-arms with a fresh ticket, so no stale wakeup can leak into a later
 * park.
 *
 * Verdict-scope: send-cases and default: are not modeled; O3 exercises
 * recv-only select, which is what the fairness question needs.
 */
public final class GoSelect {
    static final class Waiter implements GoChannel.SelSignal {
        final Goroutine g;
        final long ticket;
        final AtomicBoolean claimed = new AtomicBoolean(false);
        Waiter(Goroutine g, long ticket) { this.g = g; this.ticket = ticket; }
        @Override
        public void signal() {
            if (claimed.compareAndSet(false, true)) {
                UserScheduler.instance().wakeup(g, ticket);
            }
        }
    }

    /** Select over recv cases; blocks until one is ready. Returns its index and value. */
    public record SelResult<T>(int index, T value) {}

    public static <T> SelResult<T> selectRecv(List<GoChannel<T>> cases) {
        UserScheduler us = UserScheduler.instance();
        Goroutine g = Goroutine.current();
        List<GoChannel<T>> ordered = new ArrayList<>(cases);
        ordered.sort(Comparator.comparingLong(GoChannel::selId));
        Waiter w = null;
        for (;;) {
            List<GoChannel<T>> shuffled = new ArrayList<>(cases);
            Collections.shuffle(shuffled, ThreadLocalRandom.current());
            for (GoChannel<T> ch : shuffled) {
                GoChannel.RecvResult<T> r = ch.tryRecv();
                if (r.ok()) {
                    if (w != null) unregister(w, ordered);
                    return new SelResult<>(cases.indexOf(ch), r.value());
                }
            }
            if (w == null) {
                long t = us.armPark(g);
                w = new Waiter(g, t);
                for (GoChannel<T> ch : ordered) ch.addSelectWaiter(w);
                // Loop once more through tryRecv BEFORE parking: catches any
                // value that arrived between the first try and registration.
            } else {
                us.parkIdle(g, w.ticket);
                unregister(w, ordered);
                w = null;
            }
        }
    }

    private static <T> void unregister(Waiter w, List<GoChannel<T>> ordered) {
        for (GoChannel<T> ch : ordered) ch.removeSelectWaiter(w);
    }

    private GoSelect() {}
}
