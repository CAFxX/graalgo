package graalgo.p3link;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Map;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTables;
import graalgo.runtime.Intrinsics;
import graalgo.runtime.Linknames;
import graalgo.runtime.Timers;
import graalgo.values.GoFunc;
import graalgo.values.GoString;

/**
 * Phase-3 linkname installation (P3-M4, WS C, ADR-0004 D1/D3).
 *
 * <p>Both directions, by symbol name (SPEC §2.8):
 *
 * <ul>
 *   <li><b>Go → intrinsic.</b> The M4 gc-symbol table: gc symbol names
 *       from {@code //go:linkname} directives map to the intrinsic
 *       implementations, so identical Go source links against gc's
 *       runtime under gc and graalgo's intrinsics under graalgo:
 *       <ul>
 *         <li>{@code runtime.fastrand} → {@link Intrinsics#fastrand}
 *         <li>{@code runtime.fastrandn} → {@link Intrinsics#fastrandn}
 *         <li>{@code runtime.nanotime} → {@link Timers#nanotime}
 *       </ul>
 *       Arguments arrive in D5 boxed form; each target adapts explicitly
 *       (documented per target below).
 *   <li><b>Intrinsic → Go helper.</b>
 *       {@code graalgo.runtime.Linknames.callGo} invokes the Go function
 *       bound under a qualified Go name (see {@link #bindGoFuncs}).
 * </ul>
 *
 * <p>Also installs the {@link Intrinsics} itabFor provider (the P3-M3
 * itab machinery) and the interface method-name lists, and binds every
 * program function for {@code callGo}.
 *
 * <p>verifies: GG-0020
 */
public final class LinknameIntrinsics {
    private LinknameIntrinsics() {}

    /**
     * Installs everything linkname-related for one program build. Called
     * by the P3 launcher after {@code factory.buildProgram(json)} (and
     * after {@code IfaceMethodTable.install}, which this does not repeat).
     */
    public static void install(String jsonText, NodeFactory.Program program) {
        // P3-M4 remediation (P2-2): the static registries must be cleared
        // on every install — the launcher calls install per program build
        // (even empty), and without this a previous program's symbols,
        // intrinsics, and Go bindings would leak into this run.
        Linknames.clear();
        // Go → intrinsic: the M4 gc-symbol table. D5 argument types:
        // fastrandn takes an int32 (Integer).
        Linknames.register("runtime.fastrand", args -> Intrinsics.fastrand(), 0);
        Linknames.register("runtime.fastrandn",
                args -> Intrinsics.fastrandn(((Number) args[0]).intValue()), 1);
        Linknames.register("runtime.nanotime", args -> Timers.nanotime(), 0);
        // Intrinsic → Go helper by symbol name. D5: name arrives as
        // GoString; the single any-typed argument passes through boxed.
        Linknames.register("graalgo.runtime.Linknames.callGo", args -> {
            String name = new String(((GoString) args[0]).bytes(), StandardCharsets.UTF_8);
            Object[] rest = Arrays.copyOfRange(args, 1, args.length);
            return Linknames.callGo(name, rest);
        }, 2);
        // itabFor → the P3-M3 itab machinery (ADR-0004 D3).
        Intrinsics.setItabProvider(IfaceMethodTable::itabForDescriptors);
        // Interface method-name lists for itabForIntrinsic.
        IfaceMethodTable.installInterfaces(IfaceMethodTables.resolveInterfaces(jsonText));
        // Go function bindings for callGo.
        bindGoFuncs(jsonText, program);
    }

    /**
     * Binds every top-level program function under its qualified Go name
     * ({@code <package>.<name>}, e.g. {@code main.helper}) for
     * intrinsic→Go lookup. Method instantiations are out of M4 scope.
     */
    static void bindGoFuncs(String jsonText, NodeFactory.Program program) {
        Json.Value v = Json.parse(jsonText);
        if (!(v instanceof Json.Object doc)) {
            throw new FrontendException("document: must be a JSON object");
        }
        Json.Object pkg = doc.object("package", "document");
        String pkgName = pkg.string("name", "document.package");
        Map<Integer, GoFunc> functions = program.functions();
        for (Json.Value f : doc.array("funcs", "document").items()) {
            if (!(f instanceof Json.Object fn)
                    || !"funcdecl".equals(fn.string("kind", "document.funcs"))) {
                continue;
            }
            Json.Object fields = fn.object("fields", "document.funcs");
            int objId = fields.intValue("obj", "document.funcs.fields");
            GoFunc gf = functions.get(objId);
            if (gf == null) {
                continue; // main's GoRootNode is not in functions()
            }
            String name = fields.string("name", "document.funcs.fields");
            Linknames.bindGo(pkgName + "." + name, gf);
        }
    }
}
