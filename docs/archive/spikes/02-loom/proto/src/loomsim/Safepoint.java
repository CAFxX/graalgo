package loomsim;

/**
 * Models the back-edge safepoint yield that the Truffle AST would insert.
 *
 * Mapping to the real design (T5 §3.3): a Go `for` loop compiles to a Truffle
 * LoopNode/RepeatingNode whose back-edge executes a safepoint poll, and
 * function-entry nodes poll at the prologue. The poll reads the goroutine's
 * own preempt flag (Go: gp.preempt set by sysmon's retake); the flag is
 * per-goroutine and CLEARED when honored, so each preemption costs exactly
 * one yield — no yield storm.
 *
 * Modes:
 *   OFF    - no yield; shows Loom's native starvation pathology (control).
 *   UNCOND - Thread.yield() at every back-edge (upper-bound cost; also shows
 *            why unconditional yielding is not viable: queue flooding).
 *   POLL   - per-back-edge: read own preempt flag (volatile), yield only if
 *            set. This is the production-shaped design.
 *
 * Thread.yield() on a virtual thread unmounts the continuation and resubmits
 * it to the carrier scheduler (JDK 25 VirtualThread.tryYield; afterYield()
 * external-submits when the carrier's local queue is empty — the fair path).
 */
public final class Safepoint {
    public enum Mode { OFF, UNCOND, POLL, PARK }

    public static volatile Mode mode = Mode.POLL;

    /** Timed-park duration for PARK mode (ns). */
    static final long PARK_NANOS = Long.getLong("loomsim.parkNanos", 1000L);

    /** Diagnostics: preemptions honored. */
    public static final java.util.concurrent.atomic.AtomicLong yieldsHonored = new java.util.concurrent.atomic.AtomicLong();

    /** Called at every loop back-edge in the model workloads. */
    public static void poll() {
        switch (mode) {
            case UNCOND -> Thread.yield();
            case POLL -> {
                Goroutine g = Goroutine.current();
                if (g != null && g.preemptRequested) {
                    g.preemptRequested = false; // clear when honored: one yield per request
                    yieldsHonored.incrementAndGet();
                    Thread.yield();
                }
            }
            case PARK -> {
                // Scoped workaround experiment: instead of Thread.yield()
                // (which resubmits through the FJP LIFO local queue and gets
                // buried under spinner resubmits), timed-park. This unmounts
                // and FORCES the carrier to scan for other work.
                // Park duration from system property (default 1000ns).
                Goroutine g = Goroutine.current();
                if (g != null && g.preemptRequested) {
                    g.preemptRequested = false;
                    java.util.concurrent.locks.LockSupport.parkNanos(PARK_NANOS);
                }
            }
            default -> { /* OFF: nothing */ }
        }
    }

    private Safepoint() {}
}
