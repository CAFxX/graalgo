package loomsim;

import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Goroutine control block. One per virtual thread, registered in a global
 * set so the watchdog can request preemption per goroutine.
 *
 * This mirrors the real design (T5 §3.1): goroutine-local state (defer chain,
 * panic context, preempt flag) lives in virtual-thread-scoped holders. In the
 * Truffle AST the G would be reachable from the frame slot; here the
 * ThreadLocal models that reachability (ThreadLocal on a *virtual* thread is
 * per-goroutine state, not carrier state).
 */
public final class Goroutine {
    private static final AtomicLong nextId = new AtomicLong(1);
    private static final Set<Goroutine> all = ConcurrentHashMap.newKeySet();
    private static final ThreadLocal<Goroutine> current = new ThreadLocal<>();

    public final long id = nextId.getAndIncrement();

    /** Per-goroutine preempt request (Go: gp.preempt). Cleared when honored. */
    public volatile boolean preemptRequested = false;

    static Goroutine enter() {
        Goroutine g = new Goroutine();
        all.add(g);
        current.set(g);
        return g;
    }

    static void exit(Goroutine g) {
        current.remove();
        all.remove(g);
    }

    /** The calling goroutine's G, or null if not on a goroutine. */
    public static Goroutine current() {
        return current.get();
    }

    /** All live goroutines (watchdog iteration). */
    static Set<Goroutine> all() {
        return all;
    }

    private Goroutine() {}
}
