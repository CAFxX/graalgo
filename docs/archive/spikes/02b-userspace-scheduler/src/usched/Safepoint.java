package usched;

/**
 * Back-edge safepoint for spike 2b: the call the Truffle AST's loop
 * back-edge / function-entry nodes would emit.
 *
 * Unlike spike 02 (yield-or-timed-park through the FJP), a quantum expiry
 * here parks the vthread INDEFINITELY on the user-space scheduler and
 * requeues it. The FJP never sees the continuation unsolicited.
 *
 * Quantum check is amortized: nanoTime() every 1024 polls.
 */
public final class Safepoint {
    /** Quantum in nanos; -Dusched.quantumNs=... (default 1 ms). */
    public static final long quantumNanos = Long.getLong("usched.quantumNs", 1_000_000L);

    public static void poll() {
        Goroutine g = Goroutine.current();
        if (g == null) return;
        if ((g.pollCount++ & 1023) == 0) {
            long now = System.nanoTime();
            if (now - g.quantumStart >= quantumNanos) {
                g.quanta++;
                UserScheduler.instance().quantumPark(g);
                g.quantumStart = System.nanoTime();
            }
        }
    }

    private Safepoint() {}
}
