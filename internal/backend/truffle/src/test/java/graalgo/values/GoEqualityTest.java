package graalgo.values;

import graalgo.runtime.GoErrorValue;
import graalgo.runtime.PanicException;
import java.util.List;

/**
 * Regression tests for the 2026-11-04 equality review findings:
 * scalar complex equality/hash, IEEE struct-float equality/hash, and
 * the hash-of-unhashable-type panic for map[any] keys.
 *
 * <p>Every expectation below is a gc-measured value (go1.27.1):
 * complex components compare IEEE-wise (-0 == +0, NaN unequal);
 * struct float fields compare IEEE-wise (S{NaN} != S{NaN},
 * S{-0.0} == S{0.0}); a NaN map key never matches but each insert
 * still stores an entry (len grows per insert); inserting an
 * unhashable dynamic key into map[any] panics with
 * "runtime error: hash of unhashable type &lt;T&gt;".
 */
public final class GoEqualityTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    private static void assertEq(int want, int got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static void assertEq(Object want, Object got, String what) {
        if (want == null ? got != null : !want.equals(got)) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static GoStructDescriptor floatStruct(String name, GoKind kind) {
        return GoStructDescriptor.layout(name, List.of("X"), List.of(kind));
    }

    private static GoStruct floatStructVal(GoStructDescriptor d, long bits) {
        return new GoStruct(d, new long[]{bits}, new Object[0]);
    }

    private static GoStruct complexStructVal(GoStructDescriptor d, Object complex) {
        return new GoStruct(d, new long[0], new Object[]{complex});
    }

    public void testScalarComplexEqual() {
        // Distinct record instances with equal components must compare equal.
        assertTrue(GoEquality.equal(new GoComplex64(1, 2), new GoComplex64(1, 2)),
                "complex128 distinct instances");
        assertTrue(GoEquality.equal(new GoComplex32(1f, 2f), new GoComplex32(1f, 2f)),
                "complex64 distinct instances");
        assertTrue(!GoEquality.equal(new GoComplex64(1, 2), new GoComplex64(1, 3)),
                "complex128 differing im");
        assertTrue(!GoEquality.equal(new GoComplex32(1f, 2f), new GoComplex32(2f, 2f)),
                "complex64 differing re");
        // IEEE: -0 == +0 per component, NaN component never equal.
        assertTrue(GoEquality.equal(
                new GoComplex64(Math.copySign(0, -1), Math.copySign(0, -1)),
                new GoComplex64(0, 0)), "complex128 -0 == +0");
        assertTrue(GoEquality.equal(
                new GoComplex32(Math.copySign(0f, -1), 0f), new GoComplex32(0f, 0f)),
                "complex64 -0 == +0");
        assertTrue(!GoEquality.equal(
                new GoComplex64(Double.NaN, 0), new GoComplex64(Double.NaN, 0)),
                "complex128 NaN re unequal");
        assertTrue(!GoEquality.equal(
                new GoComplex32(0f, Float.NaN), new GoComplex32(0f, Float.NaN)),
                "complex64 NaN im unequal");
        assertTrue(!GoEquality.equal(
                new GoComplex64(0, 0), new GoComplex32(0f, 0f)),
                "complex128 != complex64 carrier");
    }

    public void testScalarComplexHashConsistent() {
        // Equal keys must hash equal: -0 components normalize to +0.
        assertEq(GoEquality.hash(new GoComplex64(0, 0)),
                GoEquality.hash(new GoComplex64(Math.copySign(0, -1), Math.copySign(0, -1))),
                "complex128 -0/+0 hash");
        assertEq(GoEquality.hash(new GoComplex32(0f, 0f)),
                GoEquality.hash(new GoComplex32(Math.copySign(0f, -1), Math.copySign(0f, -1))),
                "complex64 -0/+0 hash");
        // Same for scalar floats and float arrays (equal => same hash).
        assertEq(GoEquality.hash(0.0), GoEquality.hash(Math.copySign(0, -1)),
                "float64 -0/+0 hash");
        assertEq(GoEquality.hash(0f), GoEquality.hash(Math.copySign(0f, -1)),
                "float32 -0/+0 hash");
        assertEq(GoEquality.hash(new double[]{Math.copySign(0, -1), 1}),
                GoEquality.hash(new double[]{0, 1}), "double[] -0/+0 hash");
        assertEq(GoEquality.hash(new float[]{Math.copySign(0f, -1)}),
                GoEquality.hash(new float[]{0f}), "float[] -0/+0 hash");
        assertEq(GoEquality.hash(new GoComplex64[]{new GoComplex64(Math.copySign(0, -1), 0)}),
                GoEquality.hash(new GoComplex64[]{new GoComplex64(0, 0)}),
                "complex128[] -0/+0 hash");
    }

    public void testScalarComplexMapKey() {
        GoMap m = new GoMap();
        m.put(new GoComplex64(1, 2), "v128");
        assertEq("v128", m.get(new GoComplex64(1, 2)), "complex128 map get distinct instance");
        assertEq(1, m.size(), "complex128 map replace, not duplicate");
        m.put(new GoComplex64(1, 2), "v128b");
        assertEq("v128b", m.get(new GoComplex64(1, 2)), "complex128 map replace");
        assertEq(1, m.size(), "complex128 map size after replace");
        GoMap m32 = new GoMap();
        m32.put(new GoComplex32(3f, 4f), "v64");
        assertEq("v64", m32.get(new GoComplex32(3f, 4f)), "complex64 map get distinct instance");
        GoMap mz = new GoMap();
        mz.put(new GoComplex64(Math.copySign(0, -1), Math.copySign(0, -1)), "zero");
        assertEq("zero", mz.get(new GoComplex64(0, 0)), "complex128 -0 key found via +0");
    }

    public void testAnyHeldComplexEqual() {
        GoTypeDescriptor td = new GoTypeDescriptor("complex128", GoKind.COMPLEX128, false);
        GoInterface a = new GoInterface(td, new GoComplex64(1, 2), null);
        GoInterface b = new GoInterface(td, new GoComplex64(1, 2), null);
        assertTrue(GoEquality.equal(a, b), "any-held complex128 distinct instances");
        GoMap m = new GoMap();
        m.put(a, "held");
        assertEq("held", m.get(b), "any-held complex128 map get");
    }

    public void testStructComplexFieldEqual() {
        GoStructDescriptor d = GoStructDescriptor.layout("CS",
                List.of("C"), List.of(GoKind.COMPLEX128));
        assertTrue(complexStructVal(d, new GoComplex64(1, 2))
                .equals(complexStructVal(d, new GoComplex64(1, 2))),
                "struct complex128 field distinct instances");
        assertTrue(complexStructVal(d, new GoComplex64(Math.copySign(0, -1), 0))
                .equals(complexStructVal(d, new GoComplex64(0, 0))),
                "struct complex128 -0 == +0");
        assertTrue(!complexStructVal(d, new GoComplex64(Double.NaN, 0))
                .equals(complexStructVal(d, new GoComplex64(Double.NaN, 0))),
                "struct complex128 NaN unequal");
    }

    public void testStructFloatIeeeEqual() {
        GoStructDescriptor d64 = floatStruct("FS", GoKind.FLOAT64);
        assertTrue(!floatStructVal(d64, Double.doubleToLongBits(Double.NaN))
                .equals(floatStructVal(d64, Double.doubleToLongBits(Double.NaN))),
                "struct float64 NaN != NaN");
        assertTrue(floatStructVal(d64, Double.doubleToLongBits(Math.copySign(0, -1)))
                .equals(floatStructVal(d64, Double.doubleToLongBits(0))),
                "struct float64 -0 == +0");
        assertTrue(!floatStructVal(d64, Double.doubleToLongBits(1))
                .equals(floatStructVal(d64, Double.doubleToLongBits(2))),
                "struct float64 differing values");
        GoStructDescriptor d32 = floatStruct("F32S", GoKind.FLOAT32);
        assertTrue(!floatStructVal(d32, Float.floatToIntBits(Float.NaN))
                .equals(floatStructVal(d32, Float.floatToIntBits(Float.NaN))),
                "struct float32 NaN != NaN");
        assertTrue(floatStructVal(d32, Float.floatToIntBits(Math.copySign(0f, -1)))
                .equals(floatStructVal(d32, Float.floatToIntBits(0f))),
                "struct float32 -0 == +0");
    }

    public void testStructFloatHashConsistent() {
        GoStructDescriptor d64 = floatStruct("FS", GoKind.FLOAT64);
        assertEq(
                floatStructVal(d64, Double.doubleToLongBits(0)).hashCode(),
                floatStructVal(d64, Double.doubleToLongBits(Math.copySign(0, -1))).hashCode(),
                "struct float64 -0/+0 hashCode");
        GoStructDescriptor d32 = floatStruct("F32S", GoKind.FLOAT32);
        assertEq(
                floatStructVal(d32, Float.floatToIntBits(0f)).hashCode(),
                floatStructVal(d32, Float.floatToIntBits(Math.copySign(0f, -1))).hashCode(),
                "struct float32 -0/+0 hashCode");
    }

    public void testStructFloatMapKey() {
        GoStructDescriptor d = floatStruct("FS", GoKind.FLOAT64);
        GoMap m = new GoMap();
        m.put(floatStructVal(d, Double.doubleToLongBits(Math.copySign(0, -1))), "negzero");
        assertEq("negzero",
                m.get(floatStructVal(d, Double.doubleToLongBits(0))),
                "struct float64 -0 key found via +0");
        assertEq(1, m.size(), "struct float64 map size after -0/+0");
        // NaN struct key never matches, but each insert stores an entry (gc).
        GoMap mn = new GoMap();
        mn.put(floatStructVal(d, Double.doubleToLongBits(Double.NaN)), "n1");
        assertTrue(mn.get(floatStructVal(d, Double.doubleToLongBits(Double.NaN))) == null,
                "struct float64 NaN key never matches");
        assertEq(1, mn.size(), "struct float64 NaN key still stored");
        mn.put(floatStructVal(d, Double.doubleToLongBits(Double.NaN)), "n2");
        assertEq(2, mn.size(), "struct float64 NaN inserts accumulate");
    }

    public void testUnhashableAnyKeyPanics() {
        // Interface-wrapped slice: descriptor name is gc's type name.
        GoTypeDescriptor sliceDesc =
                new GoTypeDescriptor("[]int", GoKind.SLICE, false, false);
        GoInterface sliceKey = new GoInterface(sliceDesc, null, null);
        GoMap m = new GoMap();
        try {
            m.put(sliceKey, 1L);
            throw new AssertionError("slice any-key put must panic");
        } catch (PanicException e) {
            assertTrue(e.goValue() instanceof GoErrorValue, "panic value is GoErrorValue");
            String msg = ((GoErrorValue) e.goValue()).errorString();
            assertTrue(msg.equals("runtime error: hash of unhashable type []int"),
                    "slice panic message, got: " + msg);
        }
        assertEq(0, m.size(), "unhashable put stores nothing");
        // Bare slice carrier (no descriptor): still panics.
        GoMap m2 = new GoMap();
        try {
            m2.put(new GoSlice(new int[]{1}, 0, 1, 1), 1L);
            throw new AssertionError("bare slice put must panic");
        } catch (PanicException e) {
            String msg = ((GoErrorValue) e.goValue()).errorString();
            assertTrue(msg.equals("runtime error: hash of unhashable type []int32"),
                    "bare slice panic message, got: " + msg);
        }
        // Struct containing a slice field, via its descriptor name.
        GoTypeDescriptor structDesc =
                new GoTypeDescriptor("main.S", GoKind.STRUCT, false, false);
        GoInterface structKey = new GoInterface(structDesc, null, null);
        GoMap m3 = new GoMap();
        try {
            m3.put(structKey, 1L);
            throw new AssertionError("unhashable struct any-key put must panic");
        } catch (PanicException e) {
            String msg = ((GoErrorValue) e.goValue()).errorString();
            assertTrue(msg.equals("runtime error: hash of unhashable type main.S"),
                    "struct panic message, got: " + msg);
        }
        // Hashable any keys still work, including the nil interface key.
        GoMap m4 = new GoMap();
        m4.put(GoInterface.NIL, "nilkey");
        assertEq("nilkey", m4.get(GoInterface.NIL), "nil interface key");
        GoTypeDescriptor intDesc = new GoTypeDescriptor("int", GoKind.INT64, false);
        m4.put(new GoInterface(intDesc, 42L, null), "intkey");
        assertEq("intkey", m4.get(new GoInterface(intDesc, 42L, null)), "int any key");
    }
}
