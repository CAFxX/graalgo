package graalgo.frontend;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Red-green for the 2026-10-17 independent review findings R2 P0-2 and
 * R2 P1-9 (remediation lane rev2-clear).
 *
 * <p>R2 P0-2: the old {@code ClearNode} instanceof chain had no
 * {@code float[]} branch, so {@code clear(s)} on a {@code []float32} was a
 * silent no-op. RED (pre-fix): {@link #testClearFloat32ZeroesElements} and
 * {@link #testClearFloat32SubsliceRespectsBounds} fail (elements untouched).
 *
 * <p>R2 P1-9: the old {@code Object[]} branch filled with Java null, so
 * {@code clear} on a {@code []complex64}/{@code []complex128} produced nil
 * elements (Go has no nil complex — the no-nil-complex invariant) and on a
 * struct slice produced nil elements (the struct-slots-never-null
 * invariant). RED (pre-fix): {@link #testClearComplex64Zeroes},
 * {@link #testClearComplex128Zeroes} and
 * {@link #testClearStructSliceYieldsFreshZeroStructs} fail (null slots).
 *
 * <p>The fix unifies both ClearNodes (v2 frontend + p3builtin wire node) on
 * the single D5-carrier-aware {@link GoSlice#clearElements()}; these tests
 * exercise the v2 node end-to-end and the shared mechanism directly.
 */
public final class ClearSliceTest {

    /** A child node evaluating to a fixed value (frame is unused). */
    private static GoExprNode constTarget(Object value, GoKind kind) {
        return new GoExprNode(kind) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return value;
            }
        };
    }

    /** Runs {@code clear(target)} through the v2 frontend ClearNode. */
    private static long clear(Object value, GoKind kind) {
        return new ClearNode(constTarget(value, kind)).executeLong(null);
    }

    private static void assertFloats(float[] want, float[] got, String what) {
        if (want.length != got.length) {
            throw new AssertionError(what + ": length " + got.length
                    + ", want " + want.length);
        }
        for (int i = 0; i < want.length; i++) {
            if (Float.compare(want[i], got[i]) != 0) {
                throw new AssertionError(what + ": index " + i + " = " + got[i]
                        + ", want " + want[i]);
            }
        }
    }

    // --- R2 P0-2: float32 -------------------------------------------------

    /** Positive: clear zeroes every element of a []float32. */
    public void testClearFloat32ZeroesElements() {
        float[] backing = {1.5f, -2.5f, 3.25f, Float.MAX_VALUE};
        GoSlice s = new GoSlice(backing, 0, 4, 4);
        if (clear(s, GoKind.SLICE) != 0L) {
            throw new AssertionError("clear must return 0 (statement-only)");
        }
        assertFloats(new float[]{0f, 0f, 0f, 0f}, backing, "clear([]float32)");
    }

    /**
     * Negative (bounds): clear on a subslice zeroes only the
     * {@code [offset, offset+len)} range; backing elements outside the slice
     * are untouched.
     */
    public void testClearFloat32SubsliceRespectsBounds() {
        float[] backing = {9f, 1f, 2f, 3f, 8f};
        GoSlice s = new GoSlice(backing, 1, 3, 4);
        clear(s, GoKind.SLICE);
        assertFloats(new float[]{9f, 0f, 0f, 0f, 8f}, backing,
                "clear(s[1:4]) on []float32");
    }

    /** Negative: clear of a nil or empty slice is a no-op (gc parity). */
    public void testClearNilAndEmptySliceAreNoOps() {
        // Nil slice: a null GoSlice reference must not throw.
        clear(null, GoKind.SLICE);
        // Empty slice: backing beyond len(s)==0 is untouched.
        float[] backing = {7f, 7f};
        clear(new GoSlice(backing, 0, 0, 2), GoKind.SLICE);
        assertFloats(new float[]{7f, 7f}, backing, "clear of empty slice");
    }

    // --- R2 P1-9: complex --------------------------------------------------

    /** Positive: clear zeroes []complex64 to (0+0i), never null. */
    public void testClearComplex64Zeroes() {
        GoComplex32[] backing = {
            new GoComplex32(1f, 2f), new GoComplex32(-3f, 4f)};
        clear(new GoSlice(backing, 0, 2, 2), GoKind.SLICE);
        for (int i = 0; i < 2; i++) {
            if (backing[i] == null) {
                throw new AssertionError("clear([]complex64): slot " + i
                        + " is null — Go has no nil complex");
            }
            if (backing[i] != GoComplex32.ZERO) {
                throw new AssertionError("clear([]complex64): slot " + i
                        + " = " + backing[i] + ", want the (0+0i) singleton");
            }
        }
    }

    /** Positive: clear zeroes []complex128 to (0+0i), never null. */
    public void testClearComplex128Zeroes() {
        GoComplex64[] backing = {
            new GoComplex64(1d, 2d), new GoComplex64(-3d, 4d)};
        clear(new GoSlice(backing, 0, 2, 2), GoKind.SLICE);
        for (int i = 0; i < 2; i++) {
            if (backing[i] == null) {
                throw new AssertionError("clear([]complex128): slot " + i
                        + " is null — Go has no nil complex");
            }
            if (backing[i] != GoComplex64.ZERO) {
                throw new AssertionError("clear([]complex128): slot " + i
                        + " = " + backing[i] + ", want the (0+0i) singleton");
            }
        }
    }

    // --- R2 P1-9: struct ---------------------------------------------------

    /**
     * Positive: clear on a struct slice yields fresh zero-valued structs —
     * never null, never shared between slots, and the pre-clear values are
     * not mutated in place (slots are replaced, not rewritten).
     */
    public void testClearStructSliceYieldsFreshZeroStructs() {
        GoStructDescriptor desc = new GoStructDescriptor("T", List.of(
                new GoStructDescriptor.Field("n", GoKind.INT64,
                        true, 0, -1, null, null, null)));
        GoStruct a = GoStruct.zero(desc);
        a.setLong(0, 42L);
        GoStruct b = GoStruct.zero(desc);
        b.setLong(0, -7L);
        Object[] backing = {a, b};
        clear(new GoSlice(backing, 0, 2, 2), GoKind.SLICE);
        for (int i = 0; i < 2; i++) {
            if (!(backing[i] instanceof GoStruct gs)) {
                throw new AssertionError("clear([]T): slot " + i
                        + " is not a GoStruct (struct slots are never null)");
            }
            if (gs.getLong(0) != 0L) {
                throw new AssertionError("clear([]T): slot " + i
                        + " field n = " + gs.getLong(0) + ", want 0");
            }
        }
        // Fresh per slot: cleared slots must not alias each other...
        if (backing[0] == backing[1]) {
            throw new AssertionError(
                    "clear([]T): slots share one struct — structs are mutable "
                    + "values, cleared slots must be distinct instances");
        }
        // ...and the pre-clear structs are untouched (no in-place rewrite).
        if (a.getLong(0) != 42L || b.getLong(0) != -7L) {
            throw new AssertionError(
                    "clear([]T) mutated the pre-clear struct values");
        }
    }

    // --- Controls: reference kinds still nil; primitives still zero --------

    /**
     * Control (negative): reference-kind elements (string, interface) DO
     * clear to nil — guards the struct/complex fix against over-triggering.
     */
    public void testClearReferenceSliceNullsElements() {
        Object[] backing = {GoString.EMPTY, "x", null};
        clear(new GoSlice(backing, 0, 3, 3), GoKind.SLICE);
        for (int i = 0; i < 3; i++) {
            if (backing[i] != null) {
                throw new AssertionError("clear([]string): slot " + i
                        + " = " + backing[i] + ", want nil");
            }
        }
    }

    /** Regression pins: the primitive backings keep their zeroing. */
    public void testClearPrimitiveBackings() {
        byte[] b = {1, 2};
        clear(new GoSlice(b, 0, 2, 2), GoKind.SLICE);
        if (b[0] != 0 || b[1] != 0) {
            throw new AssertionError("clear([]int8) not zeroed");
        }
        short[] sh = {1, 2};
        clear(new GoSlice(sh, 0, 2, 2), GoKind.SLICE);
        if (sh[0] != 0 || sh[1] != 0) {
            throw new AssertionError("clear([]int16) not zeroed");
        }
        int[] in = {1, 2};
        clear(new GoSlice(in, 0, 2, 2), GoKind.SLICE);
        if (in[0] != 0 || in[1] != 0) {
            throw new AssertionError("clear([]int32) not zeroed");
        }
        long[] lo = {1L, 2L};
        clear(new GoSlice(lo, 0, 2, 2), GoKind.SLICE);
        if (lo[0] != 0L || lo[1] != 0L) {
            throw new AssertionError("clear([]int64) not zeroed");
        }
        double[] d = {1d, 2d};
        clear(new GoSlice(d, 0, 2, 2), GoKind.SLICE);
        if (d[0] != 0d || d[1] != 0d) {
            throw new AssertionError("clear([]float64) not zeroed");
        }
        boolean[] bo = {true, true};
        clear(new GoSlice(bo, 0, 2, 2), GoKind.SLICE);
        if (bo[0] || bo[1]) {
            throw new AssertionError("clear([]bool) not zeroed");
        }
    }

    // --- Map dispatch ------------------------------------------------------

    /** clear(m) deletes all entries. */
    public void testClearMapDeletesEntries() {
        GoMap m = new GoMap();
        m.put(GoString.EMPTY, 1L);
        m.put(new GoString(new byte[]{'k'}), 2L);
        clear(m, GoKind.MAP);
        if (m.size() != 0) {
            throw new AssertionError("clear(map): size = " + m.size()
                    + ", want 0");
        }
    }

    /** clear of a nil map is a no-op (gc parity). */
    public void testClearNilMapIsNoOp() {
        clear(null, GoKind.MAP);
    }
}
