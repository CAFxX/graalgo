package graalgo.p3reflect;

import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.lang.reflect.Method;
import java.util.List;
import java.util.Map;

/**
 * N-P1-5 (review 2026-10-30-15): {@code reflect.Value.CallSlice} over a
 * primitive-backed slice must spread the elements, not die with a raw Java
 * {@code ClassCastException}. The spread branch of
 * {@code ReflectOpNode.unboxArgs} cast the slice backing array to
 * {@code Object[]}, but {@code Boxing.newArray} backs {@code []int} with
 * {@code int[]}, {@code []byte} with {@code byte[]}, and so on.
 *
 * <p>These tests drive that private branch directly (same package, via
 * reflection) with a minimal installed type table: a variadic signature
 * {@code func(...T)} whose single argument is a {@code Value} holding a
 * slice. The int case uses a nonzero slice offset, so it also pins that
 * spread elements are read at {@code offset + j}, not from index 0.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectCallSliceTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Layout matching {@code ReflectValues.pack}: typ/ptr objects, flag long. */
    private static GoStructDescriptor valueDesc() {
        return GoStructDescriptor.layout("reflect.Value",
                List.of("typ", "ptr", "flag"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.UNSAFE_POINTER, GoKind.INT64));
    }

    private static ReflectTypes.TypeInfo installVariadic(String elemName) {
        UnsafeTable.install();
        String doc = """
                {"types": [
                  {"kind": "basic", "detail": {"name": "%s", "class": "%s", "untyped": false}},
                  {"kind": "slice", "detail": {"elem": 0}},
                  {"kind": "signature", "detail": {
                     "params": [{"name": "xs", "type": 1}], "results": [], "variadic": true}}
                ]}
                """.formatted(elemName, elemName);
        graalgo.frontend.Json.Value v = graalgo.frontend.Json.parse(doc);
        if (!(v instanceof graalgo.frontend.Json.Object obj)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        ReflectTypes.install(obj, Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == GoKind.FUNC && ti.variadic) {
                return ti;
            }
        }
        throw new AssertionError("no variadic FUNC TypeInfo built");
    }

    private static long sliceTok() {
        for (ReflectTypes.TypeInfo ti : ReflectTypes.table().byTok) {
            if (ti != null && ti.kind == GoKind.SLICE) {
                return ti.tok;
            }
        }
        throw new AssertionError("no SLICE TypeInfo built");
    }

    /** Calls the private spread branch: {@code unboxArgs(args, sig, true)}. */
    private static Object[] unboxSpread(GoSlice sliceValue, ReflectTypes.TypeInfo sig) {
        GoStruct arg = ReflectValues.pack(valueDesc(), sliceTok(),
                ReflectRefs.intern(sliceValue), 0);
        GoSlice args = new GoSlice(new GoStruct[]{arg}, 0, 1, 1);
        try {
            Method m = ReflectOpNode.class.getDeclaredMethod(
                    "unboxArgs", GoSlice.class, ReflectTypes.TypeInfo.class, boolean.class);
            m.setAccessible(true);
            return (Object[]) m.invoke(null, args, sig, true);
        } catch (ReflectiveOperationException e) {
            Throwable cause = e.getCause() != null ? e.getCause() : e;
            throw new AssertionError("unboxArgs threw: " + cause, cause);
        }
    }

    /**
     * N-P1-5 core case: CallSlice over {@code []int} backed by
     * {@code int[]} (with a nonzero slice offset). Before the fix this
     * threw {@code ClassCastException: [I cannot be cast to [Ljava.lang.Object;}.
     */
    public void testCallSliceOverIntSliceSpreadsElements() {
        ReflectTypes.TypeInfo sig = installVariadic("int");
        GoSlice slice = new GoSlice(new int[]{0, 10, 20, 30}, 1, 3, 3);
        Object[] out = unboxSpread(slice, sig);
        assertEquals(3, out.length, "spread of 3-element slice should yield 3 args, got length ");
        assertEquals(Integer.valueOf(10), out[0], "element 0");
        assertEquals(Integer.valueOf(20), out[1], "element 1");
        assertEquals(Integer.valueOf(30), out[2], "element 2");
    }

    /** Same defect, {@code []byte} backed by {@code byte[]}. */
    public void testCallSliceOverByteSliceSpreadsElements() {
        ReflectTypes.TypeInfo sig = installVariadic("uint8");
        GoSlice slice = new GoSlice(new byte[]{7, 8}, 0, 2, 2);
        Object[] out = unboxSpread(slice, sig);
        assertEquals(2, out.length, "spread of 2-element slice should yield 2 args, got length ");
        assertEquals(Byte.valueOf((byte) 7), out[0], "element 0");
        assertEquals(Byte.valueOf((byte) 8), out[1], "element 1");
    }

    /** Regression guard: the already-working {@code Object[]}-backed path is unchanged. */
    public void testCallSliceOverStringSliceStillSpreads() {
        ReflectTypes.TypeInfo sig = installVariadic("string");
        GoSlice slice = new GoSlice(new Object[]{"a", "b"}, 0, 2, 2);
        Object[] out = unboxSpread(slice, sig);
        assertEquals(2, out.length, "spread of 2-element slice should yield 2 args, got length ");
        assertEquals("a", out[0], "element 0");
        assertEquals("b", out[1], "element 1");
    }
}
