package graalgo.safepoint;

/**
 * GC safepoint seam (PLAN.md D8 [DECIDED]; GG-0017 seam slice).
 *
 * <p>Frozen signature: {@code graalgo.safepoint.poll()}. The AST inserts
 * calls at function entries ({@code GoRootNode}) and loop back-edges
 * (WS E); the Phase-1 implementation is a no-op. Phase 2 gives it
 * semantics (thread suspension for GC).
 */
public final class Safepoint {
    private Safepoint() {}

    /** Phase-1: no-op. Phase 2: the GC safepoint. */
    public static void poll() {
        // Intentionally empty in Phase 1.
    }
}
