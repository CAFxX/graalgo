package graalgo.p3reflect;

import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;

import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.util.Map;

/**
 * Unit tests for the MakeFunc trampoline's argument packing (reviewer-3
 * P1-9, 2026-10-18): a variadic target must hand the callback exactly
 * NumIn() Values with the trailing D5 args packed into the final []T
 * slice; arity violations must raise a Go panic ({@link PanicException}),
 * never a raw Java IndexOutOfBoundsException.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectMakeFuncPackTest {

    /** Minimal wire doc: int, string, unsafe.Pointer, []int, reflect.Value. */
    private static final String DOC = """
            {"types": [
              {"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": false}},
              {"kind": "basic", "detail": {"name": "string", "class": "string", "untyped": false}},
              {"kind": "basic", "detail": {"name": "Pointer", "class": "pointer", "untyped": false}},
              {"kind": "slice", "detail": {"elem": 0}},
              {"kind": "struct", "detail": {"fields": [
                {"name": "typ", "type": 2, "tag": "", "offset": 0},
                {"name": "ptr", "type": 2, "tag": "", "offset": 8},
                {"name": "flag", "type": 0, "tag": "", "offset": 16}]}},
              {"kind": "named", "detail": {"name": "reflect.Value", "pkgpath": "reflect",
                "underlying": 4, "methods": []}}
            ]}""";

    private static boolean installed = false;
    private static long intTok;
    private static long stringTok;
    private static long sliceTok;

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

    private static graalgo.frontend.Json.Object parseDoc(String text) {
        graalgo.frontend.Json.Value v = graalgo.frontend.Json.parse(text);
        if (!(v instanceof graalgo.frontend.Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

    private static synchronized void ensureInstalled() {
        if (installed) {
            return;
        }
        graalgo.p3unsafe.UnsafeTable.install();
        ReflectTypes.install(parseDoc(DOC), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti == null) {
                continue;
            }
            if (ti.kind == GoKind.INT64 && ti.basicName.equals("int")) {
                intTok = ti.tok;
            } else if (ti.kind == GoKind.STRING) {
                stringTok = ti.tok;
            } else if (ti.kind == GoKind.SLICE && ti.elem.kind == GoKind.INT64) {
                sliceTok = ti.tok;
            }
        }
        assertTrue(intTok >= 0 && stringTok >= 0 && sliceTok >= 0,
                "test doc did not build int/string/[]int");
        installed = true;
    }

    /** Reflective call to the private packMakeFuncArgs(TypeInfo, Object[]). */
    private static GoStruct[] pack(ReflectTypes.TypeInfo funcType, Object[] d5Args) {
        try {
            Method m = ReflectCalls.class.getDeclaredMethod(
                    "packMakeFuncArgs", ReflectTypes.TypeInfo.class, Object[].class);
            m.setAccessible(true);
            return (GoStruct[]) m.invoke(null, funcType, d5Args);
        } catch (java.lang.reflect.InvocationTargetException e) {
            Throwable cause = e.getCause();
            if (cause instanceof RuntimeException re) {
                throw re;
            }
            throw new AssertionError("unexpected checked exception", cause);
        } catch (ReflectiveOperationException e) {
            throw new AssertionError("cannot reach packMakeFuncArgs", e);
        }
    }

    private static GoString goStr(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    private static String panicMessage(PanicException pe) {
        Object v = pe.goValue();
        assertTrue(v instanceof GoString, "panic value should be a GoString, got " + v);
        return new String(((GoString) v).bytes(), StandardCharsets.UTF_8);
    }

    /**
     * P1-9 core: variadic func(string, ...int) called with 3 trailing args
     * must produce exactly 2 Values — the fixed param and the packed []int.
     * The old code threw IndexOutOfBoundsException here.
     */
    public void testVariadicPacksTrailingArgs() {
        ensureInstalled();
        long funcTok = ReflectTypes.funcOf(new long[]{stringTok, sliceTok}, new long[]{}, true);
        ReflectTypes.TypeInfo funcType = ReflectTypes.table().info(funcTok);
        assertTrue(funcType.variadic, "synthesized func should be variadic");

        GoStruct[] values = pack(funcType,
                new Object[]{goStr("s"), Long.valueOf(1), Long.valueOf(2), Long.valueOf(3)});

        assertEquals(2, values.length, "callback arg count");
        assertEquals(stringTok, ReflectValues.parts(values[0]).typ(), "fixed param tok");
        assertEquals(sliceTok, ReflectValues.parts(values[1]).typ(), "variadic slice tok");
        Object d5 = ReflectValues.unbox(values[1]);
        assertTrue(d5 instanceof GoSlice, "packed variadic should unbox to GoSlice, got " + d5);
        GoSlice sl = (GoSlice) d5;
        assertEquals(3, sl.len(), "packed slice length");
        for (int i = 0; i < 3; i++) {
            assertEquals(Long.valueOf(i + 1),
                    ReflectValues.arrayGet(sl.array(), GoKind.INT64, sl.offset() + i),
                    "packed element " + i);
        }
    }

    /** Variadic with zero trailing args: the final Value is an empty []int. */
    public void testVariadicZeroTrailingArgs() {
        ensureInstalled();
        long funcTok = ReflectTypes.funcOf(new long[]{stringTok, sliceTok}, new long[]{}, true);
        ReflectTypes.TypeInfo funcType = ReflectTypes.table().info(funcTok);

        GoStruct[] values = pack(funcType, new Object[]{goStr("s")});

        assertEquals(2, values.length, "callback arg count");
        Object d5 = ReflectValues.unbox(values[1]);
        assertTrue(d5 instanceof GoSlice, "packed variadic should unbox to GoSlice, got " + d5);
        assertEquals(0, ((GoSlice) d5).len(), "packed slice should be empty");
    }

    /** Pure-variadic func(...int): no fixed params, all args pack. */
    public void testPureVariadicPacksAllArgs() {
        ensureInstalled();
        long funcTok = ReflectTypes.funcOf(new long[]{sliceTok}, new long[]{}, true);
        ReflectTypes.TypeInfo funcType = ReflectTypes.table().info(funcTok);

        GoStruct[] values = pack(funcType,
                new Object[]{Long.valueOf(9), Long.valueOf(8)});

        assertEquals(1, values.length, "callback arg count");
        Object d5 = ReflectValues.unbox(values[0]);
        assertTrue(d5 instanceof GoSlice, "packed variadic should unbox to GoSlice, got " + d5);
        assertEquals(2, ((GoSlice) d5).len(), "packed slice length");
    }

    /**
     * Non-variadic arity violations must be Go panics with gc's messages —
     * never IndexOutOfBoundsException (too many) or a silently short
     * []Value (too few).
     */
    public void testNonVariadicArityMismatchPanics() {
        ensureInstalled();
        long funcTok = ReflectTypes.funcOf(new long[]{intTok, intTok}, new long[]{}, false);
        ReflectTypes.TypeInfo funcType = ReflectTypes.table().info(funcTok);

        try {
            pack(funcType, new Object[]{Long.valueOf(1)});
            throw new AssertionError("expected PanicException for too few args");
        } catch (PanicException pe) {
            assertTrue(panicMessage(pe).contains("too few input arguments"),
                    "unexpected panic message: " + panicMessage(pe));
        }
        try {
            pack(funcType, new Object[]{Long.valueOf(1), Long.valueOf(2), Long.valueOf(3)});
            throw new AssertionError("expected PanicException for too many args");
        } catch (PanicException pe) {
            assertTrue(panicMessage(pe).contains("too many input arguments"),
                    "unexpected panic message: " + panicMessage(pe));
        }
        // Exact arity still works.
        GoStruct[] values = pack(funcType, new Object[]{Long.valueOf(1), Long.valueOf(2)});
        assertEquals(2, values.length, "callback arg count");
    }

    /** Variadic with fewer args than fixed params: Go panic, not Java. */
    public void testVariadicTooFewPanics() {
        ensureInstalled();
        long funcTok = ReflectTypes.funcOf(new long[]{stringTok, sliceTok}, new long[]{}, true);
        ReflectTypes.TypeInfo funcType = ReflectTypes.table().info(funcTok);

        try {
            pack(funcType, new Object[]{});
            throw new AssertionError("expected PanicException for too few args");
        } catch (PanicException pe) {
            assertTrue(panicMessage(pe).contains("too few input arguments"),
                    "unexpected panic message: " + panicMessage(pe));
        }
    }
}
