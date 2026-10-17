package graalgo.safepoint;

/**
 * GC safepoint seam (PLAN.md D8 [DECIDED]; GG-0017 seam slice). Same-FQN
 * shadow (bridge B-8, inventoried in {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This class exists so the truffle module compiles against the safepoint
 * surface despite the truffle&#8596;runtime compile cycle (the runtime module
 * compiles the joint round first; the truffle module then compiles this
 * same-FQN class against it). The real implementation lives in the runtime
 * module ({@code graalgo.safepoint.Safepoint} in
 * {@code internal/backend/runtime}), where {@code poll()} performs the
 * scheduler quantum check. At launch, {@code internal/backend/graalgo-run}
 * puts {@code runtime/out/classes} <em>before</em> {@code truffle/out/classes}
 * on the classpath, so the real implementation shadows this class — this
 * file's {@code poll()} is only ever executed if the launcher classpath
 * order is violated (a launch bug), in which case safepoints silently
 * become no-ops. The AST inserts calls at function entries
 * ({@code GoRootNode}) and loop back-edges; the frozen signature is
 * {@code graalgo.safepoint.poll()}.
 */
public final class Safepoint {
    private Safepoint() {}

    /** No-op contract: the runtime module's same-FQN {@code Safepoint}
     * performs the scheduler quantum check; this class is a compile-time
     * placeholder that is shadowed at launch (see the class javadoc). */
    public static void poll() {
        // Intentionally empty: this copy is never the load-bearing one.
    }
}
