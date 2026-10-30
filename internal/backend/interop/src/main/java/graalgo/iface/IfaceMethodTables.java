package graalgo.iface;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.TypeTable;
import graalgo.p3log.P3Log;
import graalgo.values.GoFunc;

/**
 * Launcher support for interface method tables (P3-M3, WS D, ADR-0003):
 * parses the document's type table into descriptor name → (method name →
 * {@link IfaceMethodTarget}), then resolves the object ids through
 * {@code Program.functions()} into {@link GoFunc}s for
 * {@link IfaceMethodTable#install}.
 *
 * <p>Entries key by the method-table key
 * ({@link DescriptorNames#methodTableKey}): named entries by qualified
 * name (never the display name — N-P1-2: {@code a/foo.T} and
 * {@code b/foo.T} are distinct types whose display names collide),
 * pointer-to-named entries by {@code "*"+qualifiedName}, other pointer
 * and struct entries (anonymous structs with promoted methods, PKG-036
 * — e.g. {@code struct{ Reader io.Reader }}) by the factory's
 * structural descriptor name, the same name the factory mints at box
 * sites. Method entries are {@code
 * {"name": ..., "obj": ..., "index": [...]}} ({@code "recv"} is
 * informational only; {@code "index"} is the receiver adaptation path,
 * empty for direct methods).
 *
 * <p>Fail-fast: a method entry without a name/obj, or an obj id absent
 * from {@code Program.functions()}, is a {@link FrontendException} naming
 * the descriptor, method, and obj id.
 */
public final class IfaceMethodTables {
    private IfaceMethodTables() {}

    /**
     * Resolves every method obj id in {@code doc}'s type table to its
     * {@link GoFunc}.
     *
     * @param doc       the parsed frontend/v1 document (parse-once: caller
     *                  reuses NodeFactory.document())
     * @param functions obj id → GoFunc, from {@code Program.functions()}
     */
    public static Map<String, Map<String, IfaceMethodTarget>> resolve(Json.Object doc,
            Map<Integer, GoFunc> functions) {
        TypeTable types = new TypeTable(doc.array("types", "document"));
        Map<String, Map<String, MethodRef>> objIds = new LinkedHashMap<>();
        for (int id = 0; id < types.size(); id++) {
            String path = "types[" + id + "]";
            Json.Object entry = types.entry(id, path);
            String kind = entry.string("kind", path);
            // PKG-036: struct entries (anonymous structs with promoted
            // methods, e.g. struct{ Reader io.Reader }) carry "methods"
            // lists like named/pointer entries and must be installed —
            // otherwise dispatch on a boxed struct dies fatally with
            // "no method table installed for dynamic type".
            if (!"named".equals(kind) && !"pointer".equals(kind) && !"struct".equals(kind)) {
                continue;
            }
            String descName = DescriptorNames.methodTableKey(types, id);
            Map<String, MethodRef> methods = readMethods(entry, path);
            if (methods.isEmpty()) {
                continue;
            }
            // Two entries can share one method-table key (e.g. two
            // pointer entries with the same structural shape): the
            // factory canonicalizes them to one descriptor, and both
            // carry the same method set — merge. Identical duplicates
            // merge silently; a merge where the same method maps to a
            // DIFFERENT function (different obj id, receiver path, or
            // embedded-interface id) is a contradiction, not a
            // duplicate: fail fast instead of silently running the
            // wrong method (the pre-N-P1-2 last-wins behavior). This
            // check is what separates legitimate structural duplicates
            // from a genuine key collision.
            objIds.merge(descName, methods, (a, b) -> {
                Map<String, MethodRef> merged = new LinkedHashMap<>(a);
                for (Map.Entry<String, MethodRef> e : b.entrySet()) {
                    MethodRef prev = merged.put(e.getKey(), e.getValue());
                    if (prev != null && !sameMethodRef(prev, e.getValue())) {
                        throw new FrontendException("ifacemethod: conflicting method tables for \""
                                + descName + "\": method \"" + e.getKey()
                                + "\" maps to obj " + prev.objId() + " and obj "
                                + e.getValue().objId() + " (internal error)");
                    }
                }
                return merged;
            });
        }
        Map<String, Map<String, IfaceMethodTarget>> out = new LinkedHashMap<>();
        for (Map.Entry<String, Map<String, MethodRef>> e : objIds.entrySet()) {
            Map<String, IfaceMethodTarget> funcs = new LinkedHashMap<>();
            for (Map.Entry<String, MethodRef> m : e.getValue().entrySet()) {
                MethodRef ref = m.getValue();
                GoFunc f;
                List<String> embMethods;
                if (ref.embeddedIfaceId >= 0) {
                    // P3-M9: embedded-interface redispatch entry (wire
                    // "embeddedIface"): no GoFunc — the dispatcher reads
                    // the embedding path to the interface field value and
                    // dispatches the method on it.
                    if (ref.objId != -1) {
                        throw new FrontendException("ifacemethod: method \"" + m.getKey()
                                + "\" on dynamic type \"" + e.getKey()
                                + "\" has embeddedIface but obj != -1 (WS D)");
                    }
                    f = null;
                    embMethods = embeddedIfaceMethodNames(types, ref.embeddedIfaceId,
                            e.getKey(), m.getKey());
                } else {
                    f = functions.get(ref.objId);
                    if (f == null) {
                        // Disk-MR stubs are skipped by the frontend (desugar
                        // rewrites all calls to the __-helper); the method
                        // table entry is vestigial. Skip instead of failing.
                        P3Log.warn(IfaceMethodTables.class,
                                "ifacemethod: method \"" + m.getKey()
                                + "\" on dynamic type \"" + e.getKey()
                                + "\" refers to unknown function object id " + ref.objId
                                + " (skipped; likely a disk-MR stub)");
                        continue;
                    }
                    embMethods = List.of();
                }
                funcs.put(m.getKey(), new IfaceMethodTarget(f, ref.recvPath,
                        ref.pointerReceiver, ref.addrOf, ref.embeddedIfaceId, embMethods));
            }
            out.put(e.getKey(), funcs);
        }
        return out;
    }

    /** Wire method entry: object id + receiver adaptation data. */
    private record MethodRef(int objId, int[] recvPath, boolean pointerReceiver, boolean addrOf,
            int embeddedIfaceId) {}

    /**
     * Method names of the embedded interface's {@code interface}-kind
     * type-table entry (P3-M9 redispatch): the inner itab for the
     * interface field value is built from these, in declaration order.
     */
    private static List<String> embeddedIfaceMethodNames(TypeTable types, int id,
            String descName, String methodName) {
        String mp = "types[" + id + "]";
        Json.Object entry = types.entry(id, mp);
        if (!"interface".equals(entry.string("kind", mp))) {
            throw new FrontendException("ifacemethod: method \"" + methodName
                    + "\" on dynamic type \"" + descName
                    + "\" has embeddedIface id " + id + " which is not an interface type (WS D)");
        }
        return readInterfaceMethodNames(entry, mp);
    }

    /**
     * Returns the interface method-name lists from {@code doc}'s type table
     * (P3-M4, WS C, ADR-0004 D3): interface descriptor name → method names
     * in declaration order, for the {@code Intrinsics.itabFor} intrinsic.
     * Both named interfaces (keyed by their qualified name, e.g.
     * {@code main.Writer}) and anonymous ones (keyed by the factory's
     * structural name, e.g. {@code interface#3}) are covered.
     *
     * @param doc the parsed frontend/v1 document (parse-once: caller reuses
     *            NodeFactory.document())
     */
    public static Map<String, IfaceMethodTable.IfaceWireEntry> resolveInterfaces(Json.Object doc) {
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

    /** True when two wire entries for the same method are the same method (not a conflict). */
    private static boolean sameMethodRef(MethodRef a, MethodRef b) {
        return a.objId() == b.objId()
                && a.pointerReceiver() == b.pointerReceiver()
                && a.addrOf() == b.addrOf()
                && a.embeddedIfaceId() == b.embeddedIfaceId()
                && java.util.Arrays.equals(a.recvPath(), b.recvPath());
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
            // P3-M9: "addrOf" marks a pointer-receiver method promoted via a
            // value-embedded struct field — the dispatcher takes the address
            // of the promoted field value. Absent (pre-P3-M9 documents)
            // means false.
            boolean addrOf = false;
            Json.Value av = eo.opt("addrOf");
            if (av instanceof Json.Bool b) {
                addrOf = b.value();
            }
            // P3-M9: embedded-interface redispatch entries carry the
            // embedded interface's interface-kind type-table id; absent
            // (-1) on ordinary entries.
            int embeddedIfaceId = -1;
            Json.Value embv = eo.opt("embeddedIface");
            if (embv instanceof Json.Num n) {
                embeddedIfaceId = (int) n.asLong(mp);
            }
            out.put(eo.string("name", mp),
                    new MethodRef(eo.intValue("obj", mp), recvPath, pointerReceiver, addrOf,
                            embeddedIfaceId));
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
