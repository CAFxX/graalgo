package graalgo.p3reflect;

import graalgo.frontend.Json;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.runtime.Linknames;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.List;
import java.util.Map;

/**
 * R10-P1-5 (REVIEW-2026-11-10-07): {@code reflect.typedmemmove} /
 * {@code typedslicecopy} must deep-copy struct/array values (gc byte-copy
 * semantics), so the destination is field-independent of the source
 * afterwards. Tests dispatch the linknames directly from Java (Go-level
 * {@code reflect.Copy}/{@code Set} do not reach these linknames — the
 * frontend reimplements them — so the linkname seam is the test surface).
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectLinknamesCopyTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + ": expected " + expected + " but was " + actual);
        }
    }

    private static Json.Object parseDoc(String text) {
        Json.Value v = Json.parse(text);
        if (!(v instanceof Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

    /**
     * Installs the test type table and returns the tokens
     * {@code [inner, outer, sliceOuter, sliceAny, arrayOuter]} for:
     * {@code Inner{X int}}, {@code Outer{A Inner, B int}}, {@code []Outer},
     * {@code []any}, {@code [2]Outer}.
     */
    private static long[] installTypes() {
        // A fresh run table per test: handles restart from 1, like a new run.
        UnsafeTable.install();
        ReflectLinknames.registerAll();
        String doc = """
                {"types": [
                  {"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": false}},
                  {"kind": "struct", "detail": {"fields": [
                    {"name": "X", "type": 0, "tag": "", "offset": 0}]}},
                  {"kind": "struct", "detail": {"fields": [
                    {"name": "A", "type": 1, "tag": "", "offset": 0},
                    {"name": "B", "type": 0, "tag": "", "offset": 8}]}},
                  {"kind": "slice", "detail": {"elem": 2}},
                  {"kind": "interface", "detail": {"methods": []}},
                  {"kind": "slice", "detail": {"elem": 4}},
                  {"kind": "array", "detail": {"elem": 2, "len": 2}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table t = ReflectTypes.table();
        long inner = -1, outer = -1, sliceOuter = -1, sliceAny = -1, arrayOuter = -1;
        for (ReflectTypes.TypeInfo ti : t.byTok) {
            if (ti == null) {
                continue;
            }
            if (ti.kind == GoKind.STRUCT && ti.fields.size() == 1) {
                inner = ti.tok;
            } else if (ti.kind == GoKind.STRUCT && ti.fields.size() == 2) {
                outer = ti.tok;
            } else if (ti.kind == GoKind.SLICE && ti.elem.kind == GoKind.STRUCT) {
                sliceOuter = ti.tok;
            } else if (ti.kind == GoKind.SLICE && ti.elem.kind == GoKind.INTERFACE) {
                sliceAny = ti.tok;
            } else if (ti.kind == GoKind.ARRAY) {
                arrayOuter = ti.tok;
            }
        }
        assertTrue(inner > 0 && outer > 0 && sliceOuter > 0 && sliceAny > 0 && arrayOuter > 0,
                "type toks resolved: inner=" + inner + " outer=" + outer
                        + " sliceOuter=" + sliceOuter + " sliceAny=" + sliceAny
                        + " arrayOuter=" + arrayOuter);
        return new long[]{inner, outer, sliceOuter, sliceAny, arrayOuter};
    }

    /** An {@code Outer{A: Inner{X: x}, B: b}} value. */
    private static GoStruct makeOuter(long outerTok, long innerTok, long x, long b) {
        GoStruct inner = (GoStruct) ReflectOpNode.zeroFor(innerTok);
        ReflectValues.structFieldSet(inner, 0, GoKind.INT64, Long.valueOf(x));
        GoStruct outer = (GoStruct) ReflectOpNode.zeroFor(outerTok);
        ReflectValues.structFieldSet(outer, 0, GoKind.STRUCT, inner);
        ReflectValues.structFieldSet(outer, 1, GoKind.INT64, Long.valueOf(b));
        return outer;
    }

    private static long nestedX(GoStruct outer) {
        GoStruct inner = (GoStruct) ReflectValues.structFieldGet(outer, 0, GoKind.STRUCT);
        return (Long) ReflectValues.structFieldGet(inner, 0, GoKind.INT64);
    }

    private static long scalarB(GoStruct outer) {
        return (Long) ReflectValues.structFieldGet(outer, 1, GoKind.INT64);
    }

    /** A fresh cell (via {@code unsafe_New}) holding {@code value}; returns its handle. */
    private static long newCell(long tok, Object value) {
        Object h = Linknames.dispatch("reflect.unsafe_New", new Object[]{Long.valueOf(tok)});
        long handle = ((Number) h).longValue();
        ReflectRefs.Cell c = (ReflectRefs.Cell) ReflectRefs.resolve(handle);
        c.value = value;
        return handle;
    }

    private static Object cellValue(long handle) {
        return ((ReflectRefs.Cell) ReflectRefs.resolve(handle)).value;
    }

    /** An {@code unsafeheader.Slice} struct over {@code array} at {@code off}. */
    private static GoStruct sliceHeader(Object array, int off, int len, int cap) {
        GoStructDescriptor desc = GoStructDescriptor.layout("unsafeheader.Slice",
                List.of("Data", "Len", "Cap"),
                List.of(GoKind.POINTER, GoKind.INT64, GoKind.INT64));
        GoStruct h = GoStruct.zero(desc);
        ReflectValues.structFieldSet(h, 0, GoKind.POINTER, new GoPointer(array, off));
        ReflectValues.structFieldSet(h, 1, GoKind.INT64, Long.valueOf(len));
        ReflectValues.structFieldSet(h, 2, GoKind.INT64, Long.valueOf(cap));
        return h;
    }

    /**
     * typedmemmove of a struct with a nested struct field: the moved
     * value is field-independent afterwards (pre-fix the nested field
     * was shared, so mutating the destination mutated the source).
     */
    public void testTypedmemmoveDeepCopiesNestedStruct() {
        long[] toks = installTypes();
        long innerTok = toks[0], outerTok = toks[1];
        GoStruct src = makeOuter(outerTok, innerTok, 7, 3);
        long srcH = newCell(outerTok, src);
        long dstH = newCell(outerTok, ReflectOpNode.zeroFor(outerTok));
        Linknames.dispatch("reflect.typedmemmove",
                new Object[]{Long.valueOf(outerTok), Long.valueOf(dstH), Long.valueOf(srcH)});
        GoStruct dst = (GoStruct) cellValue(dstH);
        assertEq(7L, nestedX(dst), "dst nested X");
        assertEq(3L, scalarB(dst), "dst scalar B");
        assertTrue(dst != src, "dst is a distinct struct object");
        GoStruct dstInner = (GoStruct) ReflectValues.structFieldGet(dst, 0, GoKind.STRUCT);
        ReflectValues.structFieldSet(dstInner, 0, GoKind.INT64, Long.valueOf(99));
        assertEq(7L, nestedX((GoStruct) cellValue(srcH)),
                "src nested X after dst mutation");
        assertEq(99L, nestedX(dst), "dst nested X after mutation");
        // Scalar independence (already held via the store-side shallow
        // copy — pinned so a regression cannot hide behind the fix).
        ReflectValues.structFieldSet(dst, 1, GoKind.INT64, Long.valueOf(5));
        assertEq(3L, scalarB((GoStruct) cellValue(srcH)), "src scalar B after dst mutation");
    }

    /** typedmemmove of an array value deep-copies its struct elements. */
    public void testTypedmemmoveDeepCopiesArray() {
        long[] toks = installTypes();
        long innerTok = toks[0], outerTok = toks[1], arrayTok = toks[4];
        Object srcArr = ReflectValues.newArray(GoKind.STRUCT, 2);
        ReflectValues.arraySet(srcArr, GoKind.STRUCT, 0, makeOuter(outerTok, innerTok, 1, 10));
        ReflectValues.arraySet(srcArr, GoKind.STRUCT, 1, makeOuter(outerTok, innerTok, 2, 20));
        long srcH = newCell(arrayTok, srcArr);
        long dstH = newCell(arrayTok, ReflectOpNode.zeroFor(arrayTok));
        Linknames.dispatch("reflect.typedmemmove",
                new Object[]{Long.valueOf(arrayTok), Long.valueOf(dstH), Long.valueOf(srcH)});
        Object dstArr = cellValue(dstH);
        GoStruct d0 = (GoStruct) ReflectValues.arrayGet(dstArr, GoKind.STRUCT, 0);
        GoStruct d1 = (GoStruct) ReflectValues.arrayGet(dstArr, GoKind.STRUCT, 1);
        assertEq(1L, nestedX(d0), "dst[0] nested X");
        assertEq(2L, nestedX(d1), "dst[1] nested X");
        GoStruct d0Inner = (GoStruct) ReflectValues.structFieldGet(d0, 0, GoKind.STRUCT);
        ReflectValues.structFieldSet(d0Inner, 0, GoKind.INT64, Long.valueOf(99));
        GoStruct s0 = (GoStruct) ReflectValues.arrayGet((Object[]) cellValue(srcH), GoKind.STRUCT, 0);
        assertEq(1L, nestedX(s0), "src[0] nested X after dst mutation");
    }

    /** typedslicecopy of {@code []Outer}: elements are independent afterwards. */
    public void testTypedSliceCopyDeepCopiesElements() {
        long[] toks = installTypes();
        long innerTok = toks[0], outerTok = toks[1], sliceTok = toks[2];
        Object srcArr = ReflectValues.newArray(GoKind.STRUCT, 2);
        ReflectValues.arraySet(srcArr, GoKind.STRUCT, 0, makeOuter(outerTok, innerTok, 1, 10));
        ReflectValues.arraySet(srcArr, GoKind.STRUCT, 1, makeOuter(outerTok, innerTok, 2, 20));
        Object dstArr = ReflectValues.newArray(GoKind.STRUCT, 2);
        Object n = Linknames.dispatch("reflect.typedslicecopy", new Object[]{
                Long.valueOf(sliceTok), sliceHeader(dstArr, 0, 2, 2), sliceHeader(srcArr, 0, 2, 2)});
        assertEq(2L, ((Number) n).longValue(), "copied count");
        GoStruct d0 = (GoStruct) ReflectValues.arrayGet(dstArr, GoKind.STRUCT, 0);
        GoStruct d1 = (GoStruct) ReflectValues.arrayGet(dstArr, GoKind.STRUCT, 1);
        assertEq(1L, nestedX(d0), "dst[0] nested X");
        assertEq(2L, nestedX(d1), "dst[1] nested X");
        GoStruct s0 = (GoStruct) ReflectValues.arrayGet(srcArr, GoKind.STRUCT, 0);
        assertTrue(d0 != s0, "dst[0] is a copy, not the source object");
        GoStruct d0Inner = (GoStruct) ReflectValues.structFieldGet(d0, 0, GoKind.STRUCT);
        ReflectValues.structFieldSet(d0Inner, 0, GoKind.INT64, Long.valueOf(99));
        assertEq(1L, nestedX(s0), "src[0] nested X after dst mutation");
    }

    /**
     * Overlapping typedslicecopy (dst starts after src in the same
     * backing array) copies back-to-front: pre-fix the forward loop
     * overwrote the second source element with the first before reading it.
     */
    public void testTypedSliceCopyOverlapping() {
        long[] toks = installTypes();
        long innerTok = toks[0], outerTok = toks[1], sliceTok = toks[2];
        Object arr = ReflectValues.newArray(GoKind.STRUCT, 3);
        ReflectValues.arraySet(arr, GoKind.STRUCT, 0, makeOuter(outerTok, innerTok, 1, 10));
        ReflectValues.arraySet(arr, GoKind.STRUCT, 1, makeOuter(outerTok, innerTok, 2, 20));
        Object n = Linknames.dispatch("reflect.typedslicecopy", new Object[]{
                Long.valueOf(sliceTok), sliceHeader(arr, 1, 2, 3), sliceHeader(arr, 0, 2, 3)});
        assertEq(2L, ((Number) n).longValue(), "copied count");
        GoStruct a1 = (GoStruct) ReflectValues.arrayGet(arr, GoKind.STRUCT, 1);
        GoStruct a2 = (GoStruct) ReflectValues.arrayGet(arr, GoKind.STRUCT, 2);
        assertEq(1L, nestedX(a1), "arr[1] nested X after overlapping copy");
        assertEq(2L, nestedX(a2), "arr[2] nested X after overlapping copy");
    }

    /**
     * Negative control: {@code []any} elements stay header-only (shared),
     * per the F-34 precedent — the fix must not extend deep-copying to
     * interface elements.
     */
    public void testTypedSliceCopyInterfaceStaysShared() {
        long[] toks = installTypes();
        long innerTok = toks[0], outerTok = toks[1], sliceAnyTok = toks[3];
        GoStruct elem = makeOuter(outerTok, innerTok, 5, 50);
        Object srcArr = ReflectValues.newArray(GoKind.INTERFACE, 1);
        ReflectValues.arraySet(srcArr, GoKind.INTERFACE, 0, elem);
        Object dstArr = ReflectValues.newArray(GoKind.INTERFACE, 1);
        Object n = Linknames.dispatch("reflect.typedslicecopy", new Object[]{
                Long.valueOf(sliceAnyTok), sliceHeader(dstArr, 0, 1, 1), sliceHeader(srcArr, 0, 1, 1)});
        assertEq(1L, ((Number) n).longValue(), "copied count");
        assertTrue(ReflectValues.arrayGet(dstArr, GoKind.INTERFACE, 0) == elem,
                "[]any element stays shared (header-only)");
    }
}
