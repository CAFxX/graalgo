package graalgo.frontend;

import graalgo.values.GoKind;

/**
 * Red-green for review-2026-11-01-07 L3: {@code ConvertNode.executeLong}
 * must return the float→int result extended to the node's own width,
 * exactly as {@code executeInt} (extended) does — the node-protocol
 * contract {@link GoExprNode} documents ("both executeInt and
 * executeLong, with correct sign/zero extension").
 *
 * <p>Expected values are stock-gc/amd64 runtime-conversion results
 * measured with a variable (non-constant) float source — constant
 * conversions are rejected by go/types when out of range, so only
 * the runtime path is meaningful here — except the UINT64 negative
 * rows, which pin the implementation's existing rule (see UINT64).
 * The signed sub-64-bit cases
 * saturate (gc's CVTTSD2SI): {@code int32(3e9) == -2147483648}, NOT
 * the low 32 bits of the int64 conversion; the unsigned cases
 * truncate via int64: {@code uint32(5e9) == 705032704}. A fix that
 * merely masks the int64 conversion would pass the unsigned cases
 * but fail the signed saturation cases below.
 *
 * <p>Frames are null: the child is a float {@link ConstNode}, whose
 * execute methods ignore the frame, and {@code ConvertNode} itself
 * never touches the frame on the float path.
 */
public final class ConvertFloatTest {

    /** gc-measured float64 inputs, in order. */
    private static final double[] INPUTS = {
        5e9, 3e9, 300.7, 70000.9, -3e9, -1.5, 1e20,
        Double.NaN, Double.POSITIVE_INFINITY, Double.NEGATIVE_INFINITY,
        255.9, 256.9, 65535.9, 65536.9, 2147483647.9, 2147483648.0,
        4294967295.9, 4294967296.0,
    };

    /** gc-measured executeLong results, same order as INPUTS, per target. */
    private static final long[] INT8 = {
        0, 0, 44, 112, 0, -1, 0, 0, 0, 0, -1, 0, -1, 0, -1, 0, 0, 0,
    };
    private static final long[] INT16 = {
        0, 0, 300, 4464, 0, -1, 0, 0, 0, 0, 255, 256, -1, 0, -1, 0, 0, 0,
    };
    private static final long[] INT32 = {
        -2147483648L, -2147483648L, 300, 70000, -2147483648L, -1,
        -2147483648L, -2147483648L, -2147483648L, -2147483648L,
        255, 256, 65535, 65536, 2147483647L,
        -2147483648L, -2147483648L, -2147483648L,
    };
    private static final long[] UINT8 = {
        0, 0, 44, 112, 0, 255, 0, 0, 0, 0, 255, 0, 255, 0, 255, 0, 0, 0,
    };
    private static final long[] UINT16 = {
        0, 0, 300, 4464, 0, 65535, 0, 0, 0, 0, 255, 256, 65535, 0, 65535, 0, 0, 0,
    };
    private static final long[] UINT32 = {
        705032704L, 3000000000L, 300, 70000, 1294967296L, 4294967295L,
        0, 0, 0, 0, 255, 256, 65535, 65536, 2147483647L,
        2147483648L, 4294967295L, 0,
    };
    private static final long[] INT64 = {
        5000000000L, 3000000000L, 300, 70000, -3000000000L, -1,
        Long.MIN_VALUE, Long.MIN_VALUE, Long.MIN_VALUE, Long.MIN_VALUE,
        255, 256, 65535, 65536, 2147483647L,
        2147483648L, 4294967295L, 4294967296L,
    };
    /**
     * uint64 results as signed-long bit patterns, per the implementation's
     * pinned rule ({@code GoConvert.floatToUint64}, asserted by
     * {@code ValuesTest.testGoConvertFloatToInt}): negative floats
     * saturate to 0x8000000000000000. NOTE: stock gc 1.27.1/amd64 instead
     * wraps negative finite floats (uint64(-1.5) = 2^64-1) — a
     * pre-existing GoConvert divergence OUTSIDE L3's scope (L3 is the
     * executeLong masking bug; the 64-bit paths are explicitly no-op
     * there). Flagged in the lane report for a separate lane; this
     * table pins current behavior so the divergence is not silently
     * re-baselined here.
     */
    private static final long[] UINT64 = {
        5000000000L, 3000000000L, 300, 70000, Long.MIN_VALUE, Long.MIN_VALUE,
        Long.MIN_VALUE, Long.MIN_VALUE, Long.MIN_VALUE, Long.MIN_VALUE,
        255, 256, 65535, 65536, 2147483647L,
        2147483648L, 4294967295L, 4294967296L,
    };

    private static ConstNode floatConst(GoKind from, double d) {
        if (from == GoKind.FLOAT32) {
            float f = (float) d;
            return ConstNode.ofFloat(from, Float.floatToIntBits(f),
                    Double.doubleToLongBits((double) f));
        }
        return ConstNode.ofFloat(from, 0, Double.doubleToLongBits(d));
    }

    private static ConvertNode conv(GoKind from, GoKind to, double d) {
        return new ConvertNode(from, to, floatConst(from, d));
    }

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static void checkTable(GoKind to, long[] want) {
        for (int i = 0; i < INPUTS.length; i++) {
            ConvertNode n = conv(GoKind.FLOAT64, to, INPUTS[i]);
            assertEq(want[i], n.executeLong(null),
                    "float64->" + to + "(" + INPUTS[i] + ").executeLong");
        }
    }

    /** gc table for every float64→int target width. */
    public void testFloat64ToIntExecuteLongMatchesGc() {
        checkTable(GoKind.INT8, INT8);
        checkTable(GoKind.INT16, INT16);
        checkTable(GoKind.INT32, INT32);
        checkTable(GoKind.UINT8, UINT8);
        checkTable(GoKind.UINT16, UINT16);
        checkTable(GoKind.UINT32, UINT32);
        checkTable(GoKind.INT64, INT64);
        checkTable(GoKind.UINT64, UINT64);
    }

    /**
     * The node-protocol invariant: for every sub-64-bit target,
     * executeLong must equal sign/zero-extended executeInt, for both
     * float sources. This is the exact property L3 broke (the two
     * accessors of one node disagreed).
     */
    public void testExecuteLongEqualsExtendedExecuteInt() {
        GoKind[] targets = {GoKind.INT8, GoKind.INT16, GoKind.INT32,
            GoKind.UINT8, GoKind.UINT16, GoKind.UINT32};
        GoKind[] sources = {GoKind.FLOAT64, GoKind.FLOAT32};
        for (GoKind from : sources) {
            for (GoKind to : targets) {
                for (double d : INPUTS) {
                    ConvertNode n = conv(from, to, d);
                    long fromLong = n.executeLong(null);
                    int fromInt = n.executeInt(null);
                    long extended = to.isUintKind()
                            ? Integer.toUnsignedLong(fromInt) : (long) fromInt;
                    assertEq(extended, fromLong,
                            from + "->" + to + "(" + d + "): executeLong vs extended executeInt");
                }
            }
        }
    }

    /** float32 source, in-range gc values (float32 rounding stays in range). */
    public void testFloat32ToIntExecuteLong() {
        assertEq(44, conv(GoKind.FLOAT32, GoKind.INT8, 300.7).executeLong(null),
                "float32->int8(300.7)");
        assertEq(4464, conv(GoKind.FLOAT32, GoKind.INT16, 70000.9).executeLong(null),
                "float32->int16(70000.9)");
        assertEq(705032704L, conv(GoKind.FLOAT32, GoKind.UINT32, 5e9).executeLong(null),
                "float32->uint32(5e9)");
        assertEq(-2147483648L, conv(GoKind.FLOAT32, GoKind.INT32, 3e9).executeLong(null),
                "float32->int32(3e9) saturates like gc");
    }
}
