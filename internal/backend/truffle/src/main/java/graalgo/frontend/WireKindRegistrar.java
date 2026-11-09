package graalgo.frontend;

import java.util.ArrayList;
import java.util.List;
import java.util.ServiceLoader;

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
     * Discovers every provider on the classpath via {@link ServiceLoader}
     * and registers each one's builders with the given factory. This is
     * the one correct way to prepare a factory for a real program: a bare
     * {@code new NodeFactory(...)} knows only the core wire kinds, so any
     * program whose frontend JSON contains a provider kind (e.g. real fmt
     * emitting {@code ifacemethod}) dies with "unsupported expression
     * kind" on a bare factory even though the builder exists. Callers
     * keep the returned list and run {@link #afterBuild} on each provider
     * after {@link NodeFactory#buildProgram} returns.
     *
     * @param factory the node factory to register with
     * @return the discovered providers, in discovery order
     */
    static List<WireKindRegistrar> discoverAndRegister(NodeFactory factory) {
        List<WireKindRegistrar> registrars = new ArrayList<>();
        for (WireKindRegistrar registrar : ServiceLoader.load(WireKindRegistrar.class)) {
            registrars.add(registrar);
            registrar.registerAll(factory);
        }
        return registrars;
    }

    /**
     * Registers this provider's wire-kind builders with the given factory.
     *
     * @param factory the node factory to register with
     */
    void registerAll(NodeFactory factory);

    /**
     * Post-build hook, called by the launcher after {@link NodeFactory#buildProgram}
     * returns, with the built program. Providers whose per-run installs need
     * the parsed document or the function table (e.g. interop's reflect
     * type table, which needs both) install there; installs that need
     * neither stay in {@link #registerAll}. Default: no-op, so providers
     * without post-build work need not change.
     *
     * @param factory the node factory (its parsed document is available)
     * @param program the built program
     */
    default void afterBuild(NodeFactory factory, NodeFactory.Program program) {}
}
