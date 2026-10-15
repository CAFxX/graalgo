package graalgo.frontend;

/**
 * SPI for modules that provide wire-kind registrations for {@link NodeFactory}.
 *
 * <p>Decouples launchers from provider modules: a launcher in the runtime
 * module picks up registrations from the interop module (or any future
 * provider module) via {@link java.util.ServiceLoader}, without a
 * compile-time dependency on the provider. This avoids a module build cycle
 * (the provider modules build against the runtime module's classes).
 *
 * <p>Providers register themselves via a
 * {@code META-INF/services/graalgo.frontend.WireKindRegistrar} resource.
 * If no provider is on the classpath, the launcher simply gets no extra
 * registrations (the pre-provider behavior); programs using unregistered
 * wire kinds fail with the usual "unknown wire kind" error, never silently.
 */
public interface WireKindRegistrar {
    /**
     * Registers all wire kinds this provider implements on the given factory.
     *
     * @param factory the {@link NodeFactory} to register with
     */
    void registerAll(NodeFactory factory);
}
