package graalgo.p3sync.launcher;

import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.frontend.ProgramInstaller;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTables;

/**
 * {@link ProgramInstaller} provider for the interop module.
 *
 * <p>Installs the per-program registries that the product launcher
 * ({@code P3SyncMain}) installs inline, so the runtime launcher
 * ({@code graalgo.launcher.Main} — used by the differential snapshot harness)
 * gets identical behavior without a compile-time dependency on this module
 * (discovered via {@link java.util.ServiceLoader}).
 *
 * <p>Mirrors {@code P3SyncMain}'s post-build installation block exactly:
 * interface method tables, the unsafe handle table, the reflect-type table,
 * and linkname bindings. Installed on every build (even empty) so a previous
 * program's tables can never leak into this run (registries are static).
 */
public final class P3ProgramInstaller implements ProgramInstaller {
    @Override
    public void installProgram(Json.Object doc, NodeFactory.Program program) {
        // P3-M3 (WS D, ADR-0003): install the interface method tables.
        // Method objIDs resolve through Program.functions().
        IfaceMethodTable.install(IfaceMethodTables.resolve(doc, program.functions()));
        // P3-M8 (wave 2, reflect): install the GoRType table (canonical per
        // structural identity) and the unsafe handle table.
        graalgo.p3unsafe.UnsafeTable.install();
        graalgo.p3reflect.ReflectTypes.install(doc, program.functions());
        // P3-M4 (WS C, ADR-0004): linkname symbol table (both directions),
        // itabFor provider, interface method lists, and Go function bindings.
        graalgo.p3link.LinknameIntrinsics.install(doc, program);
    }
}
