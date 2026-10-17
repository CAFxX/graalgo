package graalgo.values;

import java.util.Arrays;
import java.util.List;
import java.util.Objects;

/**
 * Unit tests for {@link GoEquality}'s hash/equals contract (review N2,
 * 2026-10-18): Go's {@code ==} equates {@code +0.0} and {@code -0.0}
 * (verified against gc: {@code map[float64]int{0.0: 42}; m[-0.0]} yields
 * {@code (42, true)}), so {@link GoEquality#hash} must not distinguish
 * signed zeros — equal keys must hash equal. The {@code hash*SignedZero}
 * tests are red on the pre-fix base (they pin the contract directly);
 * the map round-trips are green regression tests (GoMap's power-of-two
 * masking happens to hide the scalar/array symptom — the hash tests are
 * the ones that fail pre-fix).
 */
public final class GoEqualityTest {

    private static void assertEq(int want, int got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

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

    private static void assertSame(Object want, Object got, String what) {
        if (!Objects.equals(want, got)) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    public void testEqualSignedZero() {
        // gc semantics: +0.0 == -0.0 for both float kinds.
        assertTrue(GoEquality.equal(0.0d, -0.0d), "equal(+0.0, -0.0)");
        assertTrue(GoEquality.equal(-0.0d, 0.0d), "equal(-0.0, +0.0)");
        assertTrue(GoEquality.equal(0.0f, -0.0f), "equal(+0.0f, -0.0f)");
        assertFalse(GoEquality.equal(0.0d, 1.0d), "equal(+0.0, 1.0)");
    }

    public void testHashSignedZeroDouble() {
        // Contract: equal keys hash equal. Red on the pre-fix base:
        // Double.hashCode distinguishes +/-0.0 (0 vs Integer.MIN_VALUE).
        assertEq(GoEquality.hash(0.0d), GoEquality.hash(-0.0d), "hash(+0.0)==hash(-0.0)");
        // The normalizer must not touch non-zero values (pins against a
        // "return constant" bodge).
        assertEq(Double.hashCode(1.5d), GoEquality.hash(1.5d), "hash(1.5) unchanged");
        assertEq(Double.hashCode(-2.25d), GoEquality.hash(-2.25d), "hash(-2.25) unchanged");
        // NaN: never equal, hash canonical (collisions only, no contract break).
        assertFalse(GoEquality.equal(Double.NaN, Double.NaN), "NaN != NaN");
        assertEq(GoEquality.hash(Double.NaN), GoEquality.hash(Double.NaN), "hash(NaN) stable");
        assertEq(Double.hashCode(Double.NaN), GoEquality.hash(Double.NaN), "hash(NaN) unchanged");
    }

    public void testHashSignedZeroFloat() {
        assertEq(GoEquality.hash(0.0f), GoEquality.hash(-0.0f), "hash(+0.0f)==hash(-0.0f)");
        assertEq(Float.hashCode(1.5f), GoEquality.hash(1.5f), "hash(1.5f) unchanged");
        assertFalse(GoEquality.equal(Float.NaN, Float.NaN), "NaNf != NaNf");
        assertEq(GoEquality.hash(Float.NaN), GoEquality.hash(Float.NaN), "hash(NaNf) stable");
    }

    public void testHashSignedZeroDoubleArray() {
        // [N]float64 map keys (arrdispatch carrier). Red on the pre-fix base:
        // Arrays.hashCode(double[]) hashes Double.hashCode per element.
        assertTrue(GoEquality.equal(new double[]{0.0, 1.0}, new double[]{-0.0, 1.0}),
                "equal([+0.0,1.0], [-0.0,1.0])");
        assertEq(GoEquality.hash(new double[]{0.0, 1.0}), GoEquality.hash(new double[]{-0.0, 1.0}),
                "hash([+0.0,1.0])==hash([-0.0,1.0])");
        // Non-zero elements keep stock Arrays.hashCode semantics exactly.
        assertEq(Arrays.hashCode(new double[]{1.0, -2.5}),
                GoEquality.hash(new double[]{1.0, -2.5}), "hash([1.0,-2.5]) unchanged");
    }

    public void testHashSignedZeroFloatArray() {
        assertTrue(GoEquality.equal(new float[]{0.0f}, new float[]{-0.0f}),
                "equal([+0.0f], [-0.0f])");
        assertEq(GoEquality.hash(new float[]{0.0f}), GoEquality.hash(new float[]{-0.0f}),
                "hash([+0.0f])==hash([-0.0f])");
        assertEq(Arrays.hashCode(new float[]{1.0f, -2.5f}),
                GoEquality.hash(new float[]{1.0f, -2.5f}), "hash([1.0f,-2.5f]) unchanged");
    }

    public void testHashSignedZeroComplexArrays() {
        // [N]complex128 carrier: equal compares re/im with !=, so
        // complex(-0.0, +0.0) == complex(+0.0, +0.0).
        GoComplex64[] a = {new GoComplex64(-0.0, 0.0)};
        GoComplex64[] b = {new GoComplex64(0.0, 0.0)};
        assertTrue(GoEquality.equal(a, b), "equal complex128 signed-zero");
        assertEq(GoEquality.hash(a), GoEquality.hash(b), "hash complex128 signed-zero");
        // [N]complex64 carrier.
        GoComplex32[] c = {new GoComplex32(-0.0f, 0.0f)};
        GoComplex32[] d = {new GoComplex32(0.0f, 0.0f)};
        assertTrue(GoEquality.equal(c, d), "equal complex64 signed-zero");
        assertEq(GoEquality.hash(c), GoEquality.hash(d), "hash complex64 signed-zero");
    }

    public void testHashSignedZeroStructKey() {
        // struct{ arr [2]float64 }: the float array rides in objects[],
        // so GoStruct.hashCode funnels through GoEquality.hash(double[]).
        GoStructDescriptor desc = GoStructDescriptor.layout("T",
                List.of("arr"), List.of(GoKind.ARRAY), List.of(2), List.of(GoKind.FLOAT64));
        GoStruct sPos = new GoStruct(desc, new long[0], new Object[]{new double[]{0.0, 1.0}});
        GoStruct sNeg = new GoStruct(desc, new long[0], new Object[]{new double[]{-0.0, 1.0}});
        assertTrue(GoEquality.equal(sPos, sNeg), "equal struct keys");
        assertEq(GoEquality.hash(sPos), GoEquality.hash(sNeg), "hash struct keys");
    }

    public void testHashSignedZeroInterfaceKey() {
        // interface key wrapping a float64 dynamic value (tier-1d).
        Object descriptor = new Object();
        GoInterface iPos = new GoInterface(descriptor, 0.0d, null);
        GoInterface iNeg = new GoInterface(descriptor, -0.0d, null);
        assertTrue(GoEquality.equal(iPos, iNeg), "equal interface keys");
        assertEq(GoEquality.hash(iPos), GoEquality.hash(iNeg), "hash interface keys");
    }

    public void testMapRoundTripSignedZeroDouble() {
        // Regression: put/get/delete/containsKey across the sign of zero.
        GoMap m = new GoMap();
        m.put(0.0d, "pos");
        assertSame("pos", m.get(-0.0d), "get(-0.0) after put(+0.0)");
        assertTrue(m.containsKey(-0.0d), "containsKey(-0.0)");
        GoMap m2 = new GoMap();
        m2.put(-0.0d, "neg");
        assertSame("neg", m2.get(0.0d), "get(+0.0) after put(-0.0)");
        // Deleting via one sign removes the entry for both.
        m.delete(-0.0d);
        assertFalse(m.containsKey(0.0d), "containsKey(+0.0) after delete(-0.0)");
        assertEq(0, m.size(), "size after delete");
        // And the map still holds exactly one entry (no duplicate keys).
        GoMap m3 = new GoMap();
        m3.put(0.0d, "a");
        m3.put(-0.0d, "b");
        assertEq(1, m3.size(), "no duplicate +/-0.0 keys");
        assertSame("b", m3.get(0.0d), "replace via -0.0 visible via +0.0");
    }

    public void testMapRoundTripSignedZeroFloatArrayKey() {
        GoMap m = new GoMap();
        m.put(new double[]{-0.0, 1.0}, "a");
        assertSame("a", m.get(new double[]{0.0, 1.0}), "get([+0.0,1.0]) after put([-0.0,1.0])");
        m.put(new double[]{0.0, 1.0}, "b");
        assertEq(1, m.size(), "no duplicate array keys");
        assertSame("b", m.get(new double[]{-0.0, 1.0}), "replace visible across sign");
    }
}
