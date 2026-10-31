package graalgo.safepoint;

/**
 * GC safepoint seam (PLAN.md D8 [DECIDED]; GG-0017 seam slice).
 *
 * <p>Frozen signature: {@code graalgo.safepoint.poll()}. The AST inserts
 * calls at function entries ({@code GoRootNode}) and loop back-edges
 * (WS E).
 *
 * <p><strong>Phase-2 implementation</strong> (P2-D8): this class
 * <em>replaces</em> the Phase-1 no-op of the same fully-qualified name.
 * The Phase-2 classpath puts {@code phase2/out/classes} before
 * {@code phase1/out/classes}, so every {@code poll()} call compiled into
 * the Phase-1 AST resolves here. The signature is byte-identical to the
 * frozen one (bodies are INTERNAL per freeze §3.5 — no ADR/version bump
 * needed); {@code FreezeSignatureTest} keeps passing.
 *
 * <p>Semantics: quantum check for the calling goroutine
 * (GG-SCHED-0001). When no scheduler is installed (plain Phase-1 runs),
 * this is a no-op — Phase-1 parity.
 */
public final class Safepoint {
    private Safepoint() {}

    /**
     * Phase-2-only diagnostic: true when THIS class (the Phase-2
     * replacement, delegating to the scheduler quantum check) is
     * loaded, absent on the Phase-1 no-op copy. The launcher calls it
     * at startup (R-02 overlay check) to fail fast if the classpath
     * order is wrong: the no-op copy would otherwise silently drop
     * scheduler quantum / safepoint semantics. NOT part of the frozen
     * contract — intentionally not covered by FreezeSignatureTest.
     */
    public static boolean isPhase2() {
        return true;
    }

    /** Phase 2: quantum-expiry check; no-op without a scheduler. */
    public static void poll() {
        graalgo.sched.SchedulerCore.pollCurrentThreadStatic();
    }
}
