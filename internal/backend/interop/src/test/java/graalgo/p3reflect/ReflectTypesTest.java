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
        private static graalgo.frontend.Json.Object parseDoc(String text) {
        graalgo.frontend.Json.Value v = graalgo.frontend.Json.parse(text);
        if (!(v instanceof graalgo.frontend.Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

public void testInvalidTypeWireEntryFailsLoud() {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"invalid type\", \"class\": \"unknown\", \"untyped\": false}}]}";
        try {
            ReflectTypes.install(parseDoc(doc), Map.of());
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
        ReflectTypes.install(parseDoc(doc), Map.of());
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
        ReflectTypes.install(parseDoc(doc), Map.of());
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
        ReflectTypes.install(parseDoc(sb.toString()), Map.of());
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
        ReflectTypes.install(parseDoc(doc), Map.of());
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

    /**
     * PKG-010: a {@code typeparam} wire entry (a type parameter in a
     * template position — e.g. the {@code [0]*T} field of the
     * {@code sync/atomic.Pointer[T]} template, whose exporter describes
     * the template's underlying struct) must build into an opaque
     * placeholder, not fail the whole table install with
     * "reflect: unsupported type kind typeparam".
     *
     * <p>A type parameter has no runtime type identity: at runtime every
     * value of the parameter's type carries its *instantiated* type (the
     * frontend monomorphizes generics), so the placeholder is null-kind
     * with unknown size/align, and its canonical key can never collide
     * with a concrete type's token.
     */
    public void testTypeparamBuildsOpaquePlaceholder() {
        String doc = """
                {"types": [
                  {"kind": "named", "detail": {
                     "name": "sync/atomic.Pointer[T]", "pkgpath": "sync/atomic",
                     "underlying": 1, "methods": []}},
                  {"kind": "struct", "detail": {"fields": [
                     {"name": "_", "type": 2, "tag": "", "offset": 0}]}},
                  {"kind": "array", "detail": {"elem": 3, "len": 0}},
                  {"kind": "pointer", "detail": {"elem": 4, "methods": []}},
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 5}},
                  {"kind": "interface", "detail": {"embedded": [], "empty": true, "methods": []}}
                ]}
                """;
        // Before the fix this threw:
        // "types[4]: reflect: unsupported type kind typeparam".
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        ReflectTypes.TypeInfo tp = null;
        ReflectTypes.TypeInfo anyIface = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti == null) {
                continue;
            }
            if (ti.kind == null) {
                assertTrue(tp == null, "two null-kind TypeInfos built for one typeparam");
                tp = ti;
            } else if (ti.kind == GoKind.INTERFACE && ti.wireId == 5) {
                anyIface = ti;
            }
        }
        assertTrue(tp != null, "no placeholder TypeInfo built for the typeparam entry");
        assertTrue(tp.wireId == 4, "placeholder wireId should be 4, got " + tp.wireId);
        assertTrue(tp.size == -1 && tp.align == -1,
                "placeholder size/align should be unknown (-1/-1), got "
                        + tp.size + "/" + tp.align);
        // No fabricated kind: kindOrdinal reports Invalid(0) for null kind.
        assertTrue(ReflectTypes.kindOrdinal(tp) == 0,
                "kindOrdinal of a typeparam should be 0 (Invalid), got "
                        + ReflectTypes.kindOrdinal(tp));
        // The placeholder must never share a token with a concrete type —
        // in particular not with its own constraint.
        assertTrue(anyIface != null, "no TypeInfo built for the any constraint");
        assertTrue(anyIface.tok != tp.tok,
                "typeparam placeholder shares a token with its constraint");
    }

    /**
     * PKG-010: typeparam canonical keys are structural (name + constraint
     * key) under a "typeparam:" prefix no concrete kind uses. Two wire
     * ids for the same parameter shape share one token; a different
     * parameter name mints a different token.
     */
    public void testTypeparamKeysAreCanonical() {
        String doc = """
                {"types": [
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 2}},
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 2}},
                  {"kind": "interface", "detail": {"embedded": [], "empty": true, "methods": []}},
                  {"kind": "typeparam", "detail": {"name": "U", "constraint": 2}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        java.util.List<ReflectTypes.TypeInfo> placeholders = new java.util.ArrayList<>();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == null) {
                placeholders.add(ti);
            }
        }
        assertTrue(placeholders.size() == 2,
                "expected 2 canonical typeparam tokens (T shared, U distinct), got "
                        + placeholders.size());
        assertTrue(placeholders.get(0).tok != placeholders.get(1).tok,
                "distinct typeparams T and U share a token");
    }

    /**
     * PKG-010: rendering a typeparam placeholder as a type string must
     * fail loud (a type parameter must never be observed at runtime — any
     * observation means the frontend leaked a template type into
     * executable code), never fabricate a type string. Cf. the P1-4
     * "invalid type" rule: fail loud, never launder into a fake kind.
     */
    public void testTypeparamStringFailsLoud() {
        String doc = """
                {"types": [
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 1}},
                  {"kind": "interface", "detail": {"embedded": [], "empty": true, "methods": []}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo tp = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == null) {
                tp = ti;
            }
        }
        assertTrue(tp != null, "no placeholder TypeInfo built for the typeparam entry");
        try {
            ReflectTypes.typeString(tp.tok);
        } catch (IllegalStateException e) {
            assertTrue(e.getMessage().contains("types[" + tp.wireId + "]"),
                    "unexpected message: " + e.getMessage());
            return;
        }
        throw new AssertionError("expected IllegalStateException from typeString on a typeparam");
    }

    /**
     * PKG-010: an F-bounded constraint cycle in the wire graph (typeparam
     * T constrained by an unnamed interface whose method signature
     * mentions T — legal Go, e.g. {@code W[T interface{ Get() T }]}) must
     * terminate key canonicalization. The re-entrancy guard cuts the
     * cycle with the parameter's own name as back-reference, mirroring
     * the frontend exporter's typeparamIDs memo-before-describe.
     */
    public void testTypeparamCyclicConstraintTerminates() {
        String doc = """
                {"types": [
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 1}},
                  {"kind": "interface", "detail": {"methods": [
                     {"name": "Get", "sig": 2}]}},
                  {"kind": "signature", "detail": {
                     "params": [], "results": [{"name": "", "type": 0}], "variadic": false}}
                ]}
                """;
        long t0 = System.nanoTime();
        ReflectTypes.install(parseDoc(doc), Map.of());
        double secs = (System.nanoTime() - t0) / 1e9;
        assertTrue(secs < 20.0,
                "install with a cyclic typeparam constraint took " + secs + "s (key recursion diverged?)");
        ReflectTypes.Table table = ReflectTypes.table();
        boolean found = false;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == null && ti.wireId == 0) {
                found = true;
            }
        }
        assertTrue(found, "no placeholder TypeInfo built for the cyclic-constraint typeparam");
    }

    /**
     * Review 2026-10-19-07 P1 (sliceTokFor): the array-slicing path must
     * mint slice tokens under the same fully-structural key as
     * {@link ReflectTypes#sliceOf}. The old kind-only key
     * ("slice:basic:ARRAY") both collided distinct unnamed composite
     * element types and diverged from SliceOf tokens, so Go-level
     * {@code reflect.Type ==} returned false where gc returns true.
     *
     * <p>Pins the canonical behavior sliceTokFor now delegates to:
     * repeated sliceOf calls share one token; structurally different
     * element types mint different tokens; the minted TypeInfo has the
     * slice shape (size 24, align 8) with the right elem.
     */
    public void testSliceOfIsStructuralAndCanonical() {
        String doc = """
                {"types": [
                  {"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": false}},
                  {"kind": "array", "detail": {"elem": 0, "len": 2}},
                  {"kind": "array", "detail": {"elem": 0, "len": 3}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        long arr2 = -1;
        long arr3 = -1;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == GoKind.ARRAY) {
                if (ti.arrayLen == 2) {
                    arr2 = ti.tok;
                } else if (ti.arrayLen == 3) {
                    arr3 = ti.tok;
                }
            }
        }
        assertTrue(arr2 != -1, "no [2]int TypeInfo built");
        assertTrue(arr3 != -1, "no [3]int TypeInfo built");
        assertTrue(arr2 != arr3, "[2]int and [3]int share a token");

        // Canonical: same element type -> same token.
        long s1 = ReflectTypes.sliceOf(arr2);
        long s2 = ReflectTypes.sliceOf(arr2);
        assertTrue(s1 == s2, "sliceOf is not canonical: " + s1 + " != " + s2);

        // Structural: different element types -> different tokens
        // (the old "slice:basic:ARRAY" key collided these).
        long s3 = ReflectTypes.sliceOf(arr3);
        assertTrue(s3 != s1,
                "sliceOf([2]int) and sliceOf([3]int) collided on token " + s1);

        // Shape: the minted slice TypeInfo matches the pre-fix layout
        // (SLICE, size 24, align 8) with the correct elem.
        ReflectTypes.TypeInfo si = table.info(s1);
        assertTrue(si.kind == GoKind.SLICE, "slice kind should be SLICE, got " + si.kind);
        assertTrue(si.size == 24 && si.align == 8,
                "slice size/align should be 24/8, got " + si.size + "/" + si.align);
        assertTrue(si.elem != null && si.elem.tok == arr2,
                "slice elem should be the [2]int token");
        ReflectTypes.TypeInfo si3 = table.info(s3);
        assertTrue(si3.elem != null && si3.elem.tok == arr3,
                "second slice elem should be the [3]int token, got "
                        + (si3.elem == null ? "null" : si3.elem.tok));
    }

    /**
     * Review 2026-10-19-07 P1, same bug family (boundMethodTok): bound
     * method values must mint func tokens under the same fully-structural
     * key as {@link ReflectTypes#funcOf}. The old ad-hoc kind-only key
     * ("func(basic:ARRAY)(basic:INT)") both collided distinct composite
     * parameter types and diverged from FuncOf tokens.
     *
     * <p>Pins the canonical behavior boundMethodTok now delegates to.
     */
    public void testFuncOfIsStructuralAndCanonical() {
        String doc = """
                {"types": [
                  {"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": false}},
                  {"kind": "array", "detail": {"elem": 0, "len": 2}},
                  {"kind": "array", "detail": {"elem": 0, "len": 3}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        long intTok = -1;
        long arr2 = -1;
        long arr3 = -1;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti == null) {
                continue;
            }
            if (ti.kind == GoKind.INT64) {
                intTok = ti.tok;
            } else if (ti.kind == GoKind.ARRAY) {
                if (ti.arrayLen == 2) {
                    arr2 = ti.tok;
                } else if (ti.arrayLen == 3) {
                    arr3 = ti.tok;
                }
            }
        }
        assertTrue(intTok != -1, "no int TypeInfo built");
        assertTrue(arr2 != -1, "no [2]int TypeInfo built");
        assertTrue(arr3 != -1, "no [3]int TypeInfo built");

        // Canonical: same signature -> same token.
        long f1 = ReflectTypes.funcOf(new long[]{arr2}, new long[]{intTok}, false);
        long f2 = ReflectTypes.funcOf(new long[]{arr2}, new long[]{intTok}, false);
        assertTrue(f1 == f2, "funcOf is not canonical: " + f1 + " != " + f2);

        // Structural: same-kind composite params with different structure
        // must not collide (the old key collided these).
        long f3 = ReflectTypes.funcOf(new long[]{arr3}, new long[]{intTok}, false);
        assertTrue(f3 != f1,
                "funcOf([2]int) and funcOf([3]int) collided on token " + f1);

        // Shape: the minted func TypeInfo matches the pre-fix layout
        // (FUNC, size 8, align 8) with receiver-stripped ins/outs.
        ReflectTypes.TypeInfo fi = table.info(f1);
        assertTrue(fi.kind == GoKind.FUNC, "func kind should be FUNC, got " + fi.kind);
        assertTrue(fi.size == 8 && fi.align == 8,
                "func size/align should be 8/8, got " + fi.size + "/" + fi.align);
        assertTrue(fi.ins.size() == 1 && fi.ins.get(0).tok == arr2,
                "func ins should be [[2]int]");
        assertTrue(fi.outs.size() == 1 && fi.outs.get(0).tok == intTok,
                "func outs should be [int]");
        assertTrue(!fi.variadic, "func should not be variadic");
    }
}
