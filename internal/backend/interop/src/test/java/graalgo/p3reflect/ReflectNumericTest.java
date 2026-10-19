package graalgo.p3reflect;

import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.List;

/**
 * Unit tests for the 2026-10-19 independent review remediation lane
 * {@code rev-reflect-numeric}:
 *
 * <ul>
 *   <li>I-5: {@code ReflectOpNode.toDouble} dropped the unsigned high bit
 *       ({@code uint64(1<<63)} converted to {@code -9.22e18}).</li>
 *   <li>I-6: {@code reflectCall} boxed only {@code t.outs.get(0)}, so
 *       {@code Call(args)[1]} on a 2-result func panicked with
 *       index-out-of-range.</li>
 * </ul>
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectNumericTest {

    static {
        // ReflectRefs.intern/load resolve through the per-run UnsafeTable;
        // install a fresh one like P3SyncMain does per run.
        graalgo.p3unsafe.UnsafeTable.install();
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + ": expected " + expected + ", got " + actual);
        }
    }

    private static void assertEquals(double expected, double actual, String msg) {
        if (Double.compare(expected, actual) != 0) {
            throw new AssertionError(msg + ": expected " + expected + ", got " + actual);
        }
    }

    /** A reflect.Value-struct descriptor (typ/ptr objects-carried, flag longs-carried). */
    private static GoStructDescriptor valueDesc() {
        return GoStructDescriptor.layout("reflect.Value",
                List.of("typ", "ptr", "flag"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.UNSAFE_POINTER, GoKind.INT64));
    }

    private static ReflectTypes.TypeInfo basicInfo(long tok, GoKind kind) {
        return new ReflectTypes.TypeInfo(tok, kind, "", "", 8, 8,
                null, null, 0, null, List.of(), List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
    }

    private static ReflectTypes.TypeInfo funcType(ReflectTypes.TypeInfo... outs) {
        return new ReflectTypes.TypeInfo(900, GoKind.FUNC, "", "", 8, 8,
                null, null, 0, null, List.of(), List.of(),
                List.of(), List.of(outs), false, List.of(), "", -1);
    }

    // ------------------------------------------------------------------
    // I-5: unsigned high bits survive Convert to float/complex.
    // ------------------------------------------------------------------

    /** uint64(1<<63) → float64 must be +9.223372036854776E18, not -9.22e18. */
    public void testUint64HighBitToFloat64() {
        Object got = ReflectOpNode.convertNumeric(
                Long.valueOf(1L << 63), GoKind.UINT64, GoKind.FLOAT64);
        double expected = 9.223372036854776E18; // 2^63, exact in double
        assertTrue(expected > 0, "sanity: unsigned double of 1<<63 is positive");
        assertEquals(expected, ((Double) got).doubleValue(), "uint64(1<<63) -> float64");
    }

    /** uint64 max → float64. */
    public void testUint64MaxToFloat64() {
        Object got = ReflectOpNode.convertNumeric(
                Long.valueOf(-1L), GoKind.UINT64, GoKind.FLOAT64);
        assertEquals(1.8446744073709552E19, ((Double) got).doubleValue(),
                "uint64 max -> float64");
    }

    /**
     * Correct-rounding discriminator for {@code unsignedLongToDouble}.
     * The old {@code (x>>>1)*2.0+(x&1)} idiom double-rounded this value to
     * {@code 2.648933169328297e+17} (1 ulp low); the hi/lo split must produce
     * the correctly-rounded {@code 2.6489331693282973e+17}. The 2^63 and
     * 2^64-1 boundary tests above are exact under both implementations and
     * would not discriminate — this test is the one that pins the rounding.
     */
    public void testUint64ToFloat64CorrectlyRounded() {
        Object got = ReflectOpNode.convertNumeric(
                Long.valueOf(0x3ad17117b280211L), GoKind.UINT64, GoKind.FLOAT64);
        assertEquals(2.6489331693282973e+17, ((Double) got).doubleValue(),
                "uint64(0x3ad17117b280211) -> float64 must be correctly rounded");
    }

    /** uint32 with the sign bit set → float64 must be positive. */
    public void testUint32SignBitToFloat64() {
        Object got = ReflectOpNode.convertNumeric(
                Integer.valueOf(0x80000000), GoKind.UINT32, GoKind.FLOAT64);
        assertEquals(2147483648.0, ((Double) got).doubleValue(),
                "uint32(0x80000000) -> float64");
    }

    /** uint32 max → float32. */
    public void testUint32MaxToFloat32() {
        Object got = ReflectOpNode.convertNumeric(
                Integer.valueOf(-1), GoKind.UINT32, GoKind.FLOAT32);
        // The float32 D5 carrier is Float: a Float/Double ternary would
        // undergo binary numeric promotion and rebox as Double.
        assertTrue(got instanceof Float,
                "float32 target must box as Float, got " + got.getClass());
        assertEquals(Float.valueOf((float) 4294967295.0), got,
                "uint32 max -> float32");
    }

    /** Complex targets route through the same toDouble (line ~1109). */
    public void testUint64HighBitToComplex128() {
        Object got = ReflectOpNode.convertNumeric(
                Long.valueOf(1L << 63), GoKind.UINT64, GoKind.COMPLEX128);
        GoComplex64 c = (GoComplex64) got;
        assertEquals(9.223372036854776E18, c.re(),
                "uint64(1<<63) -> complex128 real part");
        assertEquals(0.0, c.im(), "uint64 -> complex128 imag part");
    }

    public void testUint32MaxToComplex64() {
        Object got = ReflectOpNode.convertNumeric(
                Integer.valueOf(-1), GoKind.UINT32, GoKind.COMPLEX64);
        GoComplex32 c = (GoComplex32) got;
        assertTrue(c.re() > 0, "uint32 max -> complex64 real part is positive, got " + c.re());
        assertEquals((float) 4294967295.0, c.re(), "uint32 max -> complex64 real part");
        assertEquals(0f, c.im(), "uint32 max -> complex64 imag part");
    }

    /** Signed paths are unchanged: int64(-1) → float64 is still -1.0. */
    public void testInt64NegativeToFloat64Unchanged() {
        Object got = ReflectOpNode.convertNumeric(
                Long.valueOf(-1L), GoKind.INT64, GoKind.FLOAT64);
        assertEquals(-1.0, ((Double) got).doubleValue(), "int64(-1) -> float64");
    }

    /** Pre-masked uint8/uint16 paths are unchanged. */
    public void testUint8MaxToFloat64Unchanged() {
        Object got = ReflectOpNode.convertNumeric(
                Integer.valueOf(255), GoKind.UINT8, GoKind.FLOAT64);
        assertEquals(255.0, ((Double) got).doubleValue(), "uint8 max -> float64");
    }

    // ------------------------------------------------------------------
    // I-6: every declared out is boxed into the []Value result.
    // ------------------------------------------------------------------

    /** Two outs → two Values, each carrying its own type token. */
    public void testBoxResultsTwoOuts() {
        GoStructDescriptor desc = valueDesc();
        ReflectTypes.TypeInfo funcType = funcType(
                basicInfo(101, GoKind.INT32), basicInfo(102, GoKind.INT64));
        GoSlice slice = ReflectOpNode.boxResults(desc,
                new Object[]{Integer.valueOf(7), Long.valueOf(9)}, funcType);
        assertTrue(slice.len() == 2, "boxResults must box both outs, got len " + slice.len());
        GoStruct v0 = (GoStruct) java.lang.reflect.Array.get(slice.array(), slice.offset());
        GoStruct v1 = (GoStruct) java.lang.reflect.Array.get(slice.array(), slice.offset() + 1);
        assertTrue(ReflectValues.parts(v0).typ() == 101, "first Value carries out[0] token");
        assertTrue(ReflectValues.parts(v1).typ() == 102, "second Value carries out[1] token");
        assertEquals(Integer.valueOf(7), ReflectRefs.load(ReflectValues.parts(v0).ptr()),
                "first Value round-trips");
        assertEquals(Long.valueOf(9), ReflectRefs.load(ReflectValues.parts(v1).ptr()),
                "second Value round-trips");
    }

    /** Zero outs → empty []Value (void funcs still invoked by the caller). */
    public void testBoxResultsZeroOuts() {
        GoSlice slice = ReflectOpNode.boxResults(valueDesc(), new Object[0], funcType());
        assertTrue(slice.len() == 0, "void func boxes an empty []Value");
    }

    /** The MakeFunc callback's []Value unboxes one D5 per declared out. */
    public void testUnboxMakeFuncResultsTwo() {
        GoStructDescriptor desc = valueDesc();
        GoStruct[] values = new GoStruct[2];
        values[0] = ReflectValues.pack(desc, 101,
                ReflectRefs.intern(Integer.valueOf(42)), 0);
        values[1] = ReflectValues.pack(desc, 102,
                ReflectRefs.intern("x"), 0);
        GoSlice resultSlice = new GoSlice(values, 0, 2, 2);
        Object[] d5 = ReflectCalls.unboxMakeFuncResults(resultSlice, 2);
        assertTrue(d5.length == 2, "two D5 results");
        assertEquals(Integer.valueOf(42), d5[0], "first D5 result");
        assertEquals("x", d5[1], "second D5 result");
    }

    /** A callback result count disagreeing with the declared type fails loud. */
    public void testUnboxMakeFuncResultsCountMismatchFailsLoud() {
        GoStructDescriptor desc = valueDesc();
        GoStruct[] values = new GoStruct[1];
        values[0] = ReflectValues.pack(desc, 101,
                ReflectRefs.intern(Integer.valueOf(1)), 0);
        GoSlice resultSlice = new GoSlice(values, 0, 1, 1);
        try {
            ReflectCalls.unboxMakeFuncResults(resultSlice, 2);
        } catch (IllegalStateException e) {
            assertTrue(e.getMessage().contains("want 2"),
                    "error names the declared count: " + e.getMessage());
            return;
        }
        throw new AssertionError("expected IllegalStateException for count mismatch");
    }

    /** Legacy single-out empty []Value still maps to a nil D5 value. */
    public void testUnboxMakeFuncResultsLegacyEmptySingleOut() {
        GoSlice empty = new GoSlice(new GoStruct[0], 0, 0, 0);
        Object[] d5 = ReflectCalls.unboxMakeFuncResults(empty, 1);
        assertTrue(d5.length == 1 && d5[0] == null, "legacy empty single-out maps to nil D5");
    }

    /** Multi-out non-MakeFunc callables fail loud instead of truncating (v1 WS D). */
    public void testInvokeCallableMultiRejectsMultiOutNonMakeFunc() {
        try {
            ReflectCalls.invokeCallableMulti(new Object(), new Object[0], 2);
        } catch (IllegalStateException e) {
            assertTrue(e.getMessage().contains("2"),
                    "error names the result count: " + e.getMessage());
            return;
        }
        throw new AssertionError("expected IllegalStateException for multi-out non-MakeFunc");
    }
}
