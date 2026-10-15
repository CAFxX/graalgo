package graalgo.frontend;

/**
 * Contract for registering frontend wire-kind builders with a {@link NodeFactory}.
 *
 * <p>Implemented by modules that provide AST node builders for frontend wire
 * kinds (e.g. interop's {@code P3KindRegistrations} for the Phase-3 sync/chan/
 * time/math wire kinds). Launcher code discovers implementations via {@link
 * java.util.ServiceLoader}, so the launcher module does not need a
 * compile-time dependency on the provider module — this breaks what would
 * otherwise be a circular module dependency (provider needs the launcher's
 * runtime classes; the launcher needs the provider's registrations).
 */
public interface WireKindRegistrar {
    /**
     * Registers this provider's wire-kind builders with the given factory.
     *
     * @param factory the node factory to register with
     */
    void registerAll(NodeFactory factory);
}
