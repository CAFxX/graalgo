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
 * function object id), then resolves the object ids through
 * {@code Program.functions()} into {@link GoFunc}s for
 * {@link IfaceMethodTable#install}.
 *
 * <p>Named entries key by {@code detail.name}; pointer entries (the full
 * {@code *T} method set: value + pointer-receiver methods) key by the
 * factory's structural descriptor name via {@link DescriptorNames} — see
 * its javadoc for the recorded ADR-0003 D5 deviation
 * ({@code "pointer#"+typeId} does not match the factory's actual
 * naming). Method entries are {@code {"name": ..., "obj": ...}}
 * ({@code "recv"} is informational only).
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
    public static Map<String, Map<String, GoFunc>> resolve(String jsonText,
            Map<Integer, GoFunc> functions) {
        Json.Value v = Json.parse(jsonText);
        if (!(v instanceof Json.Object doc)) {
            throw new FrontendException("document: must be a JSON object");
        }
        TypeTable types = new TypeTable(doc.array("types", "document"));
        Map<String, Map<String, Integer>> objIds = new LinkedHashMap<>();
        for (int id = 0; id < types.size(); id++) {
            String path = "types[" + id + "]";
            Json.Object entry = types.entry(id, path);
            String kind = entry.string("kind", path);
            if (!"named".equals(kind) && !"pointer".equals(kind)) {
                continue;
            }
            String descName = DescriptorNames.descriptorName(types, id);
            Map<String, Integer> methods = readMethods(entry, path);
            if (methods.isEmpty()) {
                continue;
            }
            // Two entries can share one descriptor name (e.g. two
            // pointer entries with the same structural shape): the
            // factory canonicalizes them to one descriptor, and both
            // carry the same *T method set — merge, last wins.
            objIds.merge(descName, methods, (a, b) -> {
                Map<String, Integer> merged = new LinkedHashMap<>(a);
                merged.putAll(b);
                return merged;
            });
        }
        Map<String, Map<String, GoFunc>> out = new LinkedHashMap<>();
        for (Map.Entry<String, Map<String, Integer>> e : objIds.entrySet()) {
            Map<String, GoFunc> funcs = new LinkedHashMap<>();
            for (Map.Entry<String, Integer> m : e.getValue().entrySet()) {
                GoFunc f = functions.get(m.getValue());
                if (f == null) {
                    throw new FrontendException("ifacemethod: method \"" + m.getKey()
                            + "\" on dynamic type \"" + e.getKey()
                            + "\" refers to unknown function object id " + m.getValue()
                            + " (WS D)");
                }
                funcs.put(m.getKey(), f);
            }
            out.put(e.getKey(), funcs);
        }
        return out;
    }

    private static Map<String, Integer> readMethods(Json.Object entry, String path) {
        Json.Object detail = entry.object("detail", path);
        Json.Value mv = detail.opt("methods");
        if (mv == null || mv instanceof Json.Null) {
            return Map.of();
        }
        if (!(mv instanceof Json.Array arr)) {
            throw new FrontendException(path + ": \"methods\" must be an array");
        }
        Map<String, Integer> out = new LinkedHashMap<>();
        for (int i = 0; i < arr.size(); i++) {
            String mp = path + ".detail.methods[" + i + "]";
            Json.Value ev = arr.get(i);
            if (!(ev instanceof Json.Object eo)) {
                throw new FrontendException(mp + ": method entry must be an object");
            }
            out.put(eo.string("name", mp), eo.intValue("obj", mp));
        }
        return out;
    }
}
