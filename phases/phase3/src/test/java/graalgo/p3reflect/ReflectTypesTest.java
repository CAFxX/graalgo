package graalgo.p3reflect;

import graalgo.frontend.FrontendException;
import graalgo.values.GoKind;

import java.util.Map;

/**
 * Regression tests for reviewer-2 P1-4 (an "invalid type" wire entry must
 * fail loud, never become a fabricated kind) and P1-5 (forward references
 * must resolve to complete TypeInfos, not baked shells), plus the unsafe
 * follow-up (bare "Pointer" wire name for unsafe.Pointer).
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectTypesTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static ReflectTypes.TypeInfo findByKind(ReflectTypes.Table t, GoKind kind) {
        for (ReflectTypes.TypeInfo ti : t.byTok) {
            if (ti != null && ti.kind == kind) {
                return ti;
            }
        }
        return null;
    }

    /**
     * P1-4: a basic type named "invalid type" on the wire (types.Invalid
     * leaking from the exporter) must throw, not be laundered into a fake
     * INTERFACE kind with fabricated size/align.
     */
    public void testInvalidTypeWireEntryFailsLoud() {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"invalid type\", \"class\": \"unknown\", \"untyped\": false}}]}";
        try {
            ReflectTypes.install(doc, Map.of());
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("unknown basic type"),
                    "unexpected message: " + e.getMessage());
            return;
        }
        throw new AssertionError("expected FrontendException for \"invalid type\" wire entry");
    }

    /**
     * P1-5: a signature referencing a later-declared named type is a
     * forward reference (signature wire id &lt; named wire id). The built
     * parameter TypeInfo must be complete — kind, fields, size — not a
     * baked shell (which had kind null, empty fields, size -1).
     */
    public void testForwardReferenceBuildsCompleteTypeInfo() {
        String doc = """
                {"types": [
                  {"kind": "signature", "detail": {
                     "params": [{"name": "v", "type": 1}], "results": [], "variadic": false}},
                  {"kind": "named", "detail": {
                     "name": "main.T", "pkgpath": "main", "underlying": 2, "methods": []}},
                  {"kind": "struct", "detail": {
                     "fields": [{"name": "X", "type": 3, "tag": "", "offset": 0}]}},
                  {"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": false}}
                ]}
                """;
        ReflectTypes.install(doc, Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        ReflectTypes.TypeInfo sig = findByKind(table, GoKind.FUNC);
        assertTrue(sig != null, "no FUNC TypeInfo built");
        assertTrue(sig.ins.size() == 1, "sig should have 1 param, got " + sig.ins.size());
        ReflectTypes.TypeInfo param = sig.ins.get(0);
        assertTrue(param.tok != -1, "param is a baked shell (tok == -1)");
        assertTrue(param.kind == GoKind.STRUCT,
                "param kind should be STRUCT, got " + param.kind);
        assertTrue(param.fields.size() == 1,
                "param should have 1 field, got " + param.fields.size());
        assertTrue("X".equals(param.fields.get(0).name),
                "param field name should be X, got " + param.fields.get(0).name);
        assertTrue(param.size == 8, "param size should be 8, got " + param.size);

        ReflectTypes.TypeInfo named = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && "main.T".equals(ti.name)) {
                named = ti;
            }
        }
        assertTrue(named != null, "no named main.T TypeInfo built");
        assertTrue(named.kind == GoKind.STRUCT, "named kind should be STRUCT, got " + named.kind);
        assertTrue(named.fields.size() == 1,
                "named should have 1 field, got " + named.fields.size());
        assertTrue(named.size == 8, "named size should be 8, got " + named.size);
    }

    /**
     * Unsafe-differential follow-up: go/types names the unsafe.Pointer
     * basic "Pointer" (not "unsafe.Pointer"), and the exporter sends
     * t.Name() verbatim. The table must accept the bare wire name and
     * still report Kind() == UnsafePointer (ordinal 26) and
     * Type.String() == "unsafe.Pointer", exactly like gc.
     */
    public void testBarePointerWireNameIsUnsafePointer() {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"Pointer\", \"class\": \"pointer\", \"untyped\": false}}]}";
        ReflectTypes.install(doc, Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo ptr = findByKind(table, GoKind.UNSAFE_POINTER);
        assertTrue(ptr != null, "no UNSAFE_POINTER TypeInfo built for wire name \"Pointer\"");
        assertTrue(ReflectTypes.kindOrdinal(ptr) == 26,
                "Kind ordinal should be 26 (UnsafePointer), got " + ReflectTypes.kindOrdinal(ptr));
        assertTrue("unsafe.Pointer".equals(ReflectTypes.typeString(ptr.tok)),
                "Type.String should be \"unsafe.Pointer\", got " + ReflectTypes.typeString(ptr.tok));
        assertTrue(ptr.size == 8 && ptr.align == 8,
                "unsafe.Pointer should be size 8 align 8, got " + ptr.size + "/" + ptr.align);
    }

    /**
     * M8b os-hang follow-up: {@code build()} used to call {@code resolve()}
     * once per type id, and each call re-ran token minting + TypeInfo
     * building over all ids (the id parameter was unused) — an O(n^2) key
     * recomputation. On the synthetic os package (2297 types) build() took
     * minutes, which a 60s corpus timeout misreported as a directory-
     * iteration hang. This test pins the linear behavior: 1200 struct
     * types install in ~1s; the old loop needed ~54s for the same table.
     * Bound is 20s (13x headroom over the fixed code on a loaded VM).
     */
    public void testLargeTypeTableInstallIsLinear() {
        StringBuilder sb = new StringBuilder("{\"types\": [");
        sb.append("{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}}");
        for (int i = 0; i < 1200; i++) {
            sb.append(",{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                    + "{\"name\": \"a\", \"type\": 0, \"tag\": \"\", \"offset\": 0},"
                    + "{\"name\": \"b\", \"type\": 0, \"tag\": \"\", \"offset\": 8},"
                    + "{\"name\": \"c\", \"type\": 0, \"tag\": \"\", \"offset\": 16}"
                    + "]}}");
        }
        sb.append("]}");
        long t0 = System.nanoTime();
        ReflectTypes.install(sb.toString(), Map.of());
        double secs = (System.nanoTime() - t0) / 1e9;
        assertTrue(secs < 20.0,
                "install of 1201 types took " + secs + "s (O(n^2) resolve loop regressed?)");
        // The 1200 structs are structurally identical, so canonical sharing
        // mints one token for all of them; the timing bound above is the
        // regression pin, not the token count.
    }

    /**
     * P3-M9 (reflect-iface): {@code descriptorForTok} must build the
     * canonical descriptor on demand for a named type the frontend never
     * materialized (e.g. a type only ever touched through
     * reflect.Value.Interface()). Before the fix it threw
     * "reflect: no descriptor for token", which broke
     * {@code reflect.ValueOf(xs).Index(0).Interface()} for any named
     * element type and, downstream, fmt's Stringer dispatch on
     * reflect-obtained values.
     */
    public void testDescriptorForTokBuildsOnDemandForNamedType() {
        String doc = """
                {"types": [
                  {"kind": "named", "detail": {
                     "name": "main.S", "pkgpath": "main", "underlying": 1, "methods": []}},
                  {"kind": "basic", "detail": {"name": "string", "class": "string", "untyped": false}}
                ]}
                """;
        ReflectTypes.install(doc, Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo named = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && "main.S".equals(ti.name)) {
                named = ti;
            }
        }
        assertTrue(named != null, "no named main.S TypeInfo built");
        // Must not throw; must return the canonical descriptor whose
        // display name matches the frontend's naming.
        graalgo.values.GoTypeDescriptor d =
                ReflectTypes.descriptorForTok(named.tok);
        assertTrue(d != null, "descriptorForTok returned null for main.S");
        assertTrue("main.S".equals(d.name()),
                "descriptor name should be \"main.S\", got \"" + d.name() + "\"");
        // Identity: a second call returns the same canonical instance.
        assertTrue(d == ReflectTypes.descriptorForTok(named.tok),
                "descriptorForTok is not canonical across calls");
    }
}
