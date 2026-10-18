package graalgo.values;

import java.util.List;

/**
 * Unit tests for {@link GoStruct} equality/hash with float and complex
 * fields (review follow-up to N2, 2026-10-18): Go struct {@code ==}
 * compares fields with {@code ==}, so float fields need IEEE semantics
 * ({@code +0.0 == -0.0}, NaN != NaN — including a struct against
 * itself), not bit-exact {@code longs[]} comparison; {@code hashCode}
 * must stay consistent (equal structs hash equal), which the scalar
 * complex branches of {@link GoEquality} must also honor.
 *
 * <p>Descriptors are built once per struct type (as the frontend does):
 * {@code GoStruct.equals} requires descriptor identity, so every
 * compared pair shares one descriptor — otherwise the tests would fail
 * on descriptor identity instead of the float semantics under test.
 *
 * <p>All {@code test*} methods are red on the pre-fix base:
 * <ul>
 *   <li>float-field tests: pre-fix {@code equals} used
 *       {@code Arrays.equals(longs, ...)} (bit-exact) and
 *       {@code hashCode} used {@code Arrays.hashCode(longs)}, so
 *       signed zeros compared/hashed unequal and NaNs compared equal;
 *   <li>complex-field tests: pre-fix {@link GoEquality#equal} had no
 *       scalar {@code GoComplex32}/{@code GoComplex64} branch (distinct
 *       instances fell through to {@code false}, and the
 *       reference-identity shortcut equated NaN-carrying instances),
 *       and {@link GoEquality#hash} fell through to the identity hash.
 * </ul>
 */
public final class GoStructFloatEqTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    private static void assertFalse(boolean cond, String what) {
        if (cond) {
            throw new AssertionError(what + ": expected false");
        }
    }

    private static void assertEq(int want, int got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static GoStructDescriptor f64Desc() {
        return GoStructDescriptor.layout("F64", List.of("x"), List.of(GoKind.FLOAT64));
    }

    private static GoStructDescriptor f32Desc() {
        return GoStructDescriptor.layout("F32", List.of("x"), List.of(GoKind.FLOAT32));
    }

    /** struct{x float64} with the given IEEE bit pattern (cf. Boxing.toLongBits). */
    private static GoStruct f64(GoStructDescriptor desc, long bits) {
        return new GoStruct(desc, new long[]{bits}, new Object[0]);
    }

    /** struct{x float32} with the given IEEE bit pattern. */
    private static GoStruct f32(GoStructDescriptor desc, long bits) {
        return new GoStruct(desc, new long[]{bits}, new Object[0]);
    }

    public void testStructFloat64SignedZeroEqual() {
        // Go: struct{x float64}{+0.0} == struct{x float64}{-0.0}.
        GoStructDescriptor desc = f64Desc();
        GoStruct pos = f64(desc, Double.doubleToLongBits(0.0));
        GoStruct neg = f64(desc, Double.doubleToLongBits(-0.0));
        assertTrue(pos.equals(neg), "+0.0 struct == -0.0 struct");
        assertTrue(neg.equals(pos), "-0.0 struct == +0.0 struct (symmetric)");
        assertTrue(pos.equals(pos.copy()), "reflexive across copy");
        // Sanity: genuinely different values still compare unequal.
        assertFalse(pos.equals(f64(desc, Double.doubleToLongBits(1.0))),
                "+0.0 struct != 1.0 struct");
    }

    public void testStructFloat32SignedZeroEqual() {
        GoStructDescriptor desc = f32Desc();
        GoStruct pos = f32(desc, Float.floatToIntBits(0.0f));
        GoStruct neg = f32(desc, Float.floatToIntBits(-0.0f));
        assertTrue(pos.equals(neg), "+0.0f struct == -0.0f struct");
        assertTrue(neg.equals(pos), "-0.0f struct == +0.0f struct (symmetric)");
        assertFalse(pos.equals(f32(desc, Float.floatToIntBits(1.0f))),
                "+0.0f struct != 1.0f struct");
    }

    public void testStructFloat64NaNFieldsUnequal() {
        // Go: NaN != NaN, so structs with NaN float fields are never
        // equal — even bit-identical ones, even a struct against itself.
        GoStructDescriptor desc = f64Desc();
        long nanBits = Double.doubleToLongBits(Double.NaN);
        GoStruct a = f64(desc, nanBits);
        GoStruct b = f64(desc, nanBits);
        assertFalse(a.equals(b), "bit-identical NaN structs unequal");
        assertFalse(a.equals(a), "NaN struct != itself (Go: s == s is false)");
        assertFalse(b.equals(a), "symmetric NaN inequality");
        // NaN vs a real value is unequal too.
        assertFalse(a.equals(f64(desc, Double.doubleToLongBits(0.0))),
                "NaN struct != +0.0 struct");
    }

    public void testStructFloat32NaNFieldsUnequal() {
        GoStructDescriptor desc = f32Desc();
        long nanBits = Float.floatToIntBits(Float.NaN);
        GoStruct a = f32(desc, nanBits);
        GoStruct b = f32(desc, nanBits);
        assertFalse(a.equals(b), "bit-identical NaNf structs unequal");
        assertFalse(a.equals(a), "NaNf struct != itself");
    }

    public void testStructMixedIntFloatFields() {
        // struct{n int64, x float64}: the int slot stays bit-exact while
        // the float slot uses IEEE semantics.
        GoStructDescriptor desc = GoStructDescriptor.layout("M",
                List.of("n", "x"), List.of(GoKind.INT64, GoKind.FLOAT64));
        GoStruct base = new GoStruct(desc,
                new long[]{1L, Double.doubleToLongBits(0.0)}, new Object[0]);
        GoStruct negZero = new GoStruct(desc,
                new long[]{1L, Double.doubleToLongBits(-0.0)}, new Object[0]);
        GoStruct otherInt = new GoStruct(desc,
                new long[]{2L, Double.doubleToLongBits(0.0)}, new Object[0]);
        GoStruct nan = new GoStruct(desc,
                new long[]{1L, Double.doubleToLongBits(Double.NaN)}, new Object[0]);
        GoStruct nan2 = new GoStruct(desc,
                new long[]{1L, Double.doubleToLongBits(Double.NaN)}, new Object[0]);
        assertTrue(base.equals(negZero), "{1,+0.0} == {1,-0.0}");
        assertFalse(base.equals(otherInt), "{1,+0.0} != {2,+0.0}");
        assertFalse(base.equals(nan), "{1,+0.0} != {1,NaN}");
        assertFalse(nan.equals(nan2), "{1,NaN} != {1,NaN}");
        assertTrue(base.equals(base.deepCopy()), "deep copy equals original");
    }

    public void testStructHashCodeConsistentWithEquals() {
        // Contract: a.equals(b) ⇒ a.hashCode() == b.hashCode().
        GoStructDescriptor desc = f64Desc();
        GoStruct pos = f64(desc, Double.doubleToLongBits(0.0));
        GoStruct neg = f64(desc, Double.doubleToLongBits(-0.0));
        assertTrue(pos.equals(neg), "precondition: equal");
        assertEq(pos.hashCode(), neg.hashCode(), "hashCode(+0.0 struct)==hashCode(-0.0 struct)");
        GoStructDescriptor desc32 = f32Desc();
        GoStruct pos32 = f32(desc32, Float.floatToIntBits(0.0f));
        GoStruct neg32 = f32(desc32, Float.floatToIntBits(-0.0f));
        assertTrue(pos32.equals(neg32), "precondition: equal (float32)");
        assertEq(pos32.hashCode(), neg32.hashCode(), "hashCode(+0.0f struct)==hashCode(-0.0f struct)");
        // Non-zero values keep hashing exactly like before (pins against a
        // "return constant" bodge: 31*idHash + (31*1 + Long.hashCode(bits))).
        GoStruct one = f64(desc, Double.doubleToLongBits(1.5));
        int want = 31 * System.identityHashCode(desc)
                + (31 * 1 + Long.hashCode(Double.doubleToLongBits(1.5)));
        assertEq(want, one.hashCode(), "hashCode(1.5 struct) unchanged");
    }

    public void testStructMapRoundTripAcrossZeroSign() {
        // Map keys differing only in the float zero sign must hit the same
        // entry (needs equals AND hashCode fixed together).
        GoStructDescriptor desc = GoStructDescriptor.layout("K",
                List.of("x"), List.of(GoKind.FLOAT64));
        GoMap m = new GoMap();
        GoStruct putKey = new GoStruct(desc,
                new long[]{Double.doubleToLongBits(0.0)}, new Object[0]);
        GoStruct getKey = new GoStruct(desc,
                new long[]{Double.doubleToLongBits(-0.0)}, new Object[0]);
        m.put(putKey, "v");
        assertTrue("v".equals(m.get(getKey)), "get(-0.0 key) after put(+0.0 key)");
        assertTrue(m.containsKey(getKey), "containsKey(-0.0 key)");
        // No duplicate entry for the opposite sign.
        m.put(getKey, "w");
        assertEq(1, m.size(), "no duplicate +/-0.0 struct keys");
        assertTrue("w".equals(m.get(putKey)), "replace visible via +0.0 key");
    }

    public void testStructComplex64FieldEquality() {
        // complex64 rides in objects[] as a GoComplex32 scalar.
        GoStructDescriptor desc = GoStructDescriptor.layout("C64",
                List.of("c"), List.of(GoKind.COMPLEX64));
        GoStruct a = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(1.5f, -2.5f)});
        GoStruct b = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(1.5f, -2.5f)});
        assertTrue(a.equals(b), "distinct equal complex64 instances equal");
        assertTrue(b.equals(a), "symmetric");
        GoStruct negZero = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(-0.0f, 0.0f)});
        GoStruct posZero = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(0.0f, 0.0f)});
        assertTrue(negZero.equals(posZero), "complex64 signed-zero components equal");
        // IEEE: a NaN component makes the whole comparison false.
        GoStruct nanRe = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(Float.NaN, 0.0f)});
        GoStruct nanRe2 = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(Float.NaN, 0.0f)});
        assertFalse(nanRe.equals(nanRe2), "complex64 NaN component unequal");
        assertFalse(nanRe.equals(nanRe), "complex64 NaN struct != itself");
        GoStruct diff = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(1.5f, 2.5f)});
        assertFalse(a.equals(diff), "different complex64 values unequal");
    }

    public void testStructComplex128FieldEquality() {
        GoStructDescriptor desc = GoStructDescriptor.layout("C128",
                List.of("c"), List.of(GoKind.COMPLEX128));
        GoStruct a = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex64(1.5, -2.5)});
        GoStruct b = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex64(1.5, -2.5)});
        assertTrue(a.equals(b), "distinct equal complex128 instances equal");
        GoStruct nanIm = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex64(0.0, Double.NaN)});
        GoStruct nanIm2 = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex64(0.0, Double.NaN)});
        assertFalse(nanIm.equals(nanIm2), "complex128 NaN component unequal");
        assertFalse(nanIm.equals(nanIm), "complex128 NaN struct != itself");
    }

    public void testStructComplexHashCodeConsistent() {        GoStructDescriptor desc = GoStructDescriptor.layout("C64",
                List.of("c"), List.of(GoKind.COMPLEX64));
        GoStruct a = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(-0.0f, 0.0f)});
        GoStruct b = new GoStruct(desc, new long[0],
                new Object[]{new GoComplex32(0.0f, -0.0f)});
        assertTrue(a.equals(b), "precondition: equal");
        assertEq(a.hashCode(), b.hashCode(), "hashCode equal for equal complex structs");
        // And GoEquality.hash agrees with GoStruct.hashCode on the scalar.
        assertEq(GoEquality.hash(a), a.hashCode(), "GoEquality.hash(struct)==struct.hashCode");
    }

    public void testSelfComparisonWithHiddenNaN() {
        // GoEquality.equal's reference-identity shortcut must not equate
        // a composite with itself when NaN hides inside (differential
        // probe vs gc, review 2026-10-18): Go's == is false there.
        GoStructDescriptor desc = f64Desc();
        GoStruct nanStruct = f64(desc, Double.doubleToLongBits(Double.NaN));
        assertFalse(GoEquality.equal(nanStruct, nanStruct),
                "equal(nanStruct, nanStruct) is false");
        GoStruct zeroStruct = f64(desc, Double.doubleToLongBits(0.0));
        assertTrue(GoEquality.equal(zeroStruct, zeroStruct),
                "equal(zeroStruct, zeroStruct) is true (no NaN)");

        double[] nanArr = {1.0, Double.NaN};
        assertFalse(GoEquality.equal(nanArr, nanArr), "equal(nanArr, nanArr) is false");
        double[] okArr = {1.0, 2.0};
        assertTrue(GoEquality.equal(okArr, okArr), "equal(okArr, okArr) is true");

        float[] nanArrF = {Float.NaN};
        assertFalse(GoEquality.equal(nanArrF, nanArrF), "equal(nanArrF, nanArrF) is false");

        GoComplex64[] nanCArr = {new GoComplex64(Double.NaN, 0.0)};
        assertFalse(GoEquality.equal(nanCArr, nanCArr), "equal(nanCArr, nanCArr) is false");

        // Interface wrapping a NaN struct: same reference, still unequal.
        GoInterface i = new GoInterface(new Object(), nanStruct, null);
        assertFalse(GoEquality.equal(i, i), "equal(nanIface, nanIface) is false");
        GoInterface j = new GoInterface(new Object(), zeroStruct, null);
        assertTrue(GoEquality.equal(j, j), "equal(zeroIface, zeroIface) is true");
    }
}
