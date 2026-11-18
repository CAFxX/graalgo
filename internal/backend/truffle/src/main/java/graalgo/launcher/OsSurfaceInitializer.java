package graalgo.launcher;

/**
 * Contract for initializing the Go os surface (argv, env overlay, virtual
 * cwd) for one program run.
 *
 * <p>Implemented by modules that own the os surface (interop's {@code
 * GoOs}); the plain launcher ({@link Main}) discovers implementations via
 * {@link java.util.ServiceLoader}, so the launcher module does not need a
 * compile-time dependency on the provider module — this breaks what would
 * otherwise be a circular module dependency (the provider needs the
 * launcher's runtime classes; the launcher needs the provider's os
 * surface). Same shape as {@code graalgo.frontend.WireKindRegistrar}.
 */
public interface OsSurfaceInitializer {
    /**
     * (Re)initializes the os surface for one program run.
     *
     * @param argv the program's argv: {@code argv[0]} is the absolute,
     *     normalized path of the program entry point (gc parity: argv[0]
     *     is absolute under {@code go run}; graalgo has no binary
     *     artifact, so the entry-point file path is the honest
     *     equivalent), the rest are the launcher arguments after
     *     {@code --}. Called before Go initialization runs, so {@code
     *     os.Args} is already visible to package-var initializers.
     */
    void initialize(String[] argv);
}
