package graalgo.frontend;

/**
 * Contract for installing per-program registries after {@link NodeFactory#buildProgram}.
 *
 * <p>Some backend registries are populated from the built program's document
 * and function table (interface method tables, the unsafe handle table, the
 * reflect-type table, linkname bindings). The product launcher
 * ({@code P3SyncMain}) installs them inline; the runtime launcher
 * ({@code graalgo.launcher.Main}) cannot reference the provider module at
 * compile time (circular module dependency), so providers implement this
 * interface and are discovered via {@link java.util.ServiceLoader} — the
 * same pattern as {@link WireKindRegistrar}.
 *
 * <p>Installed on every build (even empty) so a previous program's tables can
 * never leak into this run (registries are static).
 */
public interface ProgramInstaller {
    /**
     * Installs this provider's per-program registries.
     *
     * @param doc     the parsed frontend/v1 document (from {@link NodeFactory#document()})
     * @param program the built program (function table for objID resolution)
     */
    void installProgram(Json.Object doc, NodeFactory.Program program);
}
