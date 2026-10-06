package graalgo.iface;

import java.util.LinkedHashMap;
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
 * <p>Named entries key by {@code detail.name}; pointer entries (the full
 * {@code *T} method set: value + pointer-receiver methods) key by the
 * factory's structural descriptor name via {@link DescriptorNames} — see
 * its javadoc for the recorded ADR-0003 D5 deviation
 * ({@code "pointer#"+typeId} does not match the factory's actual
 * naming). Method entries are {@code {"name": ..., "obj": ...,
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
                funcs.put(m.getKey(), new IfaceMethodTarget(f, m.getValue().recvPath));
            }
            out.put(e.getKey(), funcs);
        }
        return out;
    }

    /** Wire method entry: object id + receiver adaptation path. */
    private record MethodRef(int objId, int[] recvPath) {}

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
            out.put(eo.string("name", mp), new MethodRef(eo.intValue("obj", mp), recvPath));
        }
        return out;
    }

    /**
     * The wire {@code "index"} field → receiver adaptation path.
     * Missing (pre-promotion documents) means a direct method.
     */
    private static int[] readIndex(Json.Object eo, String mp) {
        Json.Value iv = eo.opt("index");
        if (iv == null || iv instanceof Json.Null) {
            return new int[0];
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
