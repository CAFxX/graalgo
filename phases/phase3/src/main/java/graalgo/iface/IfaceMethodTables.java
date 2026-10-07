package graalgo.iface;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.TypeTable;
import graalgo.values.GoFunc;

/**
 * Launcher support for interface method tables (P3-M3, WS D, ADR-0003):
 * parses the document's type table into descriptor name → (method name →
 * {@link IfaceMethodTarget}), then resolves the object ids through
 * {@code Program.functions()} into {@link GoFunc}s for
 * {@link IfaceMethodTable#install}.
 *
 * <p>Named entries key by the display name ({@code displayNameFor});
 * pointer-to-named entries (the full {@code *T} method set: value +
 * pointer-receiver methods) key by {@code "*"+qualifiedName}; other
 * pointer entries key by the factory's structural descriptor name — all
 * via {@link DescriptorNames}, which mirrors the factory's
 * {@code typeDescriptorFor} naming (see its javadoc for the recorded
 * ADR-0003 D5 deviation). Method entries are {@code {"name": ..., "obj": ...,
 * "index": [...]}} ({@code "recv"} is informational only;
 * {@code "index"} is the receiver adaptation path, empty for direct
 * methods).
 *
 * <p>Fail-fast: a method entry without a name/obj, or an obj id absent
 * from {@code Program.functions()}, is a {@link FrontendException} naming
 * the descriptor, method, and obj id.
 */
public final class IfaceMethodTables {
    private IfaceMethodTables() {}

    /**
     * Parses {@code jsonText}'s type table and resolves every method obj id
     * to its {@link GoFunc}.
     *
     * @param jsonText  the frontend/v1 document
     * @param functions obj id → GoFunc, from {@code Program.functions()}
     */
    public static Map<String, Map<String, IfaceMethodTarget>> resolve(String jsonText,
            Map<Integer, GoFunc> functions) {
        Json.Value v = Json.parse(jsonText);
        if (!(v instanceof Json.Object doc)) {
            throw new FrontendException("document: must be a JSON object");
        }
        TypeTable types = new TypeTable(doc.array("types", "document"));
        Map<String, Map<String, MethodRef>> objIds = new LinkedHashMap<>();
        for (int id = 0; id < types.size(); id++) {
            String path = "types[" + id + "]";
            Json.Object entry = types.entry(id, path);
            String kind = entry.string("kind", path);
            if (!"named".equals(kind) && !"pointer".equals(kind)) {
                continue;
            }
            String descName = DescriptorNames.descriptorName(types, id);
            Map<String, MethodRef> methods = readMethods(entry, path);
            if (methods.isEmpty()) {
                continue;
            }
            // Two entries can share one descriptor name (e.g. two
            // pointer entries with the same structural shape): the
            // factory canonicalizes them to one descriptor, and both
            // carry the same *T method set — merge, last wins.
            objIds.merge(descName, methods, (a, b) -> {
                Map<String, MethodRef> merged = new LinkedHashMap<>(a);
                merged.putAll(b);
                return merged;
            });
        }
        Map<String, Map<String, IfaceMethodTarget>> out = new LinkedHashMap<>();
        for (Map.Entry<String, Map<String, MethodRef>> e : objIds.entrySet()) {
            Map<String, IfaceMethodTarget> funcs = new LinkedHashMap<>();
            for (Map.Entry<String, MethodRef> m : e.getValue().entrySet()) {
                GoFunc f = functions.get(m.getValue().objId);
                if (f == null) {
                    throw new FrontendException("ifacemethod: method \"" + m.getKey()
                            + "\" on dynamic type \"" + e.getKey()
                            + "\" refers to unknown function object id " + m.getValue().objId
                            + " (WS D)");
                }
                funcs.put(m.getKey(), new IfaceMethodTarget(f, m.getValue().recvPath,
                        m.getValue().pointerReceiver));
            }
            out.put(e.getKey(), funcs);
        }
        return out;
    }

    /** Wire method entry: object id + receiver adaptation data. */
    private record MethodRef(int objId, int[] recvPath, boolean pointerReceiver) {}

    /**
     * Parses {@code jsonText}'s type table and returns the interface
     * method-name lists (P3-M4, WS C, ADR-0004 D3): interface descriptor
     * name → method names in declaration order, for the
     * {@code Intrinsics.itabFor} intrinsic. Both named interfaces (keyed
     * by their qualified name, e.g. {@code main.Writer}) and anonymous
     * ones (keyed by the factory's structural name, e.g.
     * {@code interface#3}) are covered.
     */
    public static Map<String, IfaceMethodTable.IfaceWireEntry> resolveInterfaces(String jsonText) {
        Json.Value v = Json.parse(jsonText);
        if (!(v instanceof Json.Object doc)) {
            throw new FrontendException("document: must be a JSON object");
        }
        TypeTable types = new TypeTable(doc.array("types", "document"));
        Map<String, IfaceMethodTable.IfaceWireEntry> out = new LinkedHashMap<>();
        for (int id = 0; id < types.size(); id++) {
            String path = "types[" + id + "]";
            Json.Object entry = types.entry(id, path);
            String kind = entry.string("kind", path);
            if ("interface".equals(kind)) {
                List<String> names = readInterfaceMethodNames(entry, path);
                out.put(DescriptorNames.descriptorName(types, id),
                        new IfaceMethodTable.IfaceWireEntry(id, names));
            } else if ("named".equals(kind)) {
                Json.Object detail = entry.object("detail", path);
                Json.Value underlying = detail.opt("underlying");
                if (underlying instanceof Json.Num num) {
                    int uid = (int) num.asLong(path + ".detail.underlying");
                    String upath = "types[" + uid + "]";
                    Json.Object uentry = types.entry(uid, upath);
                    if ("interface".equals(uentry.string("kind", upath))) {
                        List<String> names = readInterfaceMethodNames(uentry, upath);
                        // The wire id is the *interface* entry's id (uid):
                        // the dispatch path's ifacemethod "iface" field
                        // always points at an "interface"-kind entry.
                        // P3-M7R2: key by the display name (fa099f2), not
                        // the qualified name — the lookup side uses
                        // iface.name(), the descriptor's display name.
                        out.put(graalgo.frontend.NodeFactory.displayNameFor(
                                        detail.string("name", path)),
                                new IfaceMethodTable.IfaceWireEntry(uid, names));
                    }
                }
            }
        }
        return out;
    }

    private static List<String> readInterfaceMethodNames(Json.Object entry, String path) {
        Json.Object detail = entry.object("detail", path);
        Json.Value mv = detail.opt("methods");
        List<String> names = new ArrayList<>();
        if (mv instanceof Json.Array arr) {
            for (int i = 0; i < arr.size(); i++) {
                String mp = path + ".detail.methods[" + i + "]";
                Json.Value ev = arr.get(i);
                if (!(ev instanceof Json.Object eo)) {
                    throw new FrontendException(mp + ": method entry must be an object");
                }
                names.add(eo.string("name", mp));
            }
        }
        return names;
    }

    private static Map<String, MethodRef> readMethods(Json.Object entry, String path) {
        Json.Object detail = entry.object("detail", path);
        Json.Value mv = detail.opt("methods");
        if (mv == null || mv instanceof Json.Null) {
            return Map.of();
        }
        if (!(mv instanceof Json.Array arr)) {
            throw new FrontendException(path + ": \"methods\" must be an array");
        }
        Map<String, MethodRef> out = new LinkedHashMap<>();
        for (int i = 0; i < arr.size(); i++) {
            String mp = path + ".detail.methods[" + i + "]";
            Json.Value ev = arr.get(i);
            if (!(ev instanceof Json.Object eo)) {
                throw new FrontendException(mp + ": method entry must be an object");
            }
            int[] recvPath = readIndex(eo, mp);
            boolean pointerReceiver = "pointer".equals(eo.string("recv", mp));
            out.put(eo.string("name", mp),
                    new MethodRef(eo.intValue("obj", mp), recvPath, pointerReceiver));
        }
        return out;
    }

    /**
     * The wire {@code "index"} field → receiver adaptation path.
     * Missing (pre-promotion documents) means a direct method; explicit
     * JSON null marks an unsupported promotion (lazy fail-fast at itab
     * build, not here — the table is per program, the dispatch is per
     * call).
     */
    private static int[] readIndex(Json.Object eo, String mp) {
        Json.Value iv = eo.opt("index");
        if (iv == null) {
            return new int[0];
        }
        if (iv instanceof Json.Null) {
            return null;
        }
        if (!(iv instanceof Json.Array arr)) {
            throw new FrontendException(mp + ": \"index\" must be an array");
        }
        int[] out = new int[arr.size()];
        for (int i = 0; i < arr.size(); i++) {
            Json.Value e = arr.get(i);
            if (!(e instanceof Json.Num n)) {
                throw new FrontendException(mp + ": \"index\" entries must be numbers");
            }
            out[i] = (int) n.asLong(mp);
        }
        return out;
    }
}
