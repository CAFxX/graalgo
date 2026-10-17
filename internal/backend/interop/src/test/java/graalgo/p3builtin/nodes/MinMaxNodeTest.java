package graalgo.p3builtin.nodes;

import java.lang.reflect.Constructor;

import graalgo.frontend.ConstNode;
import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * Unit tests for the {@code "minmax"} wire node ({@link MinMaxNode}).
 *
 * <p>verifies: RV-P0-08 (2026-10-17): the float folds must implement the
 * signed-zero tie rule — {@code min} prefers {@code -0.0}, {@code max}
 * prefers {@code +0.0} — matching gc. The previous
 * {@code v < best}/{@code v > best} fold kept whichever zero arrived first,
 * which is bit-observable via {@code math.Signbit}/{@code math.Float64bits}.
 *
 * <p>Positive coverage pins the gc-parity zero rule, the NaN propagation,
 * infinity ordering and the multi-argument folds; negative coverage pins
 * that NaN still wins over signed zero (the tie rule must not outrank the
 * NaN check) and that untouched branches (ints, strings, single argument)
 * did not regress.
 */
public final class MinMaxNodeTest {

    // ---- construction ----

    private static MinMaxNode build(GoKind kind, boolean isMin, GoExprNode[] args) {
        try {
            Constructor<MinMaxNode> ctor = MinMaxNode.class.getDeclaredConstructor(
                    GoKind.class, boolean.class, GoExprNode[].class);
            ctor.setAccessible(true);
            return ctor.newInstance(kind, isMin, args);
        } catch (ReflectiveOperationException e) {
            throw new AssertionError("cannot construct MinMaxNode", e);
        }
    }

    private static MinMaxNode min(GoKind kind, GoExprNode... args) {
        return build(kind, true, args);
    }

    private static MinMaxNode max(GoKind kind, GoExprNode... args) {
        return build(kind, false, args);
    }

    private static GoExprNode f64(double v) {
        return ConstNode.ofFloat(GoKind.FLOAT64, 0, Double.doubleToRawLongBits(v));
    }

    private static GoExprNode f32(float v) {
        return ConstNode.ofFloat(GoKind.FLOAT32, Float.floatToRawIntBits(v), 0L);
    }

    private static GoExprNode i64(long v) {
        return ConstNode.ofInt(GoKind.INT64, v);
    }

    private static GoExprNode u64(long v) {
        return ConstNode.ofInt(GoKind.UINT64, v);
    }

    private static GoExprNode str(String v) {
        return ConstNode.ofString(GoString.fromUtf8(v));
    }

    // ---- assertion helpers (bit-exact: -0.0 != +0.0 under ==) ----

    private static void assertBitsEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want bits 0x" + Long.toHexString(want)
                    + " got bits 0x" + Long.toHexString(got));
        }
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    // ---- RV-P0-08: signed-zero tie rule, float64 ----

    public void testMinF64PosZeroThenNegZero() {
        double r = min(GoKind.FLOAT64, f64(+0.0), f64(-0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(+0.0, -0.0) must be -0.0");
    }

    public void testMinF64NegZeroThenPosZero() {
        double r = min(GoKind.FLOAT64, f64(-0.0), f64(+0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(-0.0, +0.0) must be -0.0");
    }

    public void testMaxF64NegZeroThenPosZero() {
        double r = max(GoKind.FLOAT64, f64(-0.0), f64(+0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(+0.0), Double.doubleToRawLongBits(r),
                "max(-0.0, +0.0) must be +0.0");
    }

    public void testMaxF64PosZeroThenNegZero() {
        double r = max(GoKind.FLOAT64, f64(+0.0), f64(-0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(+0.0), Double.doubleToRawLongBits(r),
                "max(+0.0, -0.0) must be +0.0");
    }

    public void testMinF64ThreeArgsZeroMix() {
        double r = min(GoKind.FLOAT64, f64(+0.0), f64(-0.0), f64(-0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(+0.0, -0.0, -0.0) must be -0.0");
    }

    public void testMaxF64ThreeArgsZeroMix() {
        double r = max(GoKind.FLOAT64, f64(-0.0), f64(+0.0), f64(+0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(+0.0), Double.doubleToRawLongBits(r),
                "max(-0.0, +0.0, +0.0) must be +0.0");
    }

    public void testMinF64SingleNegZero() {
        double r = min(GoKind.FLOAT64, f64(-0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(-0.0) must return its argument unchanged");
    }

    // ---- RV-P0-08: signed-zero tie rule, float32 ----

    public void testMinF32SignedZero() {
        float r = min(GoKind.FLOAT32, f32(+0.0f), f32(-0.0f)).executeFloat(null);
        assertBitsEq(Float.floatToRawIntBits(-0.0f), Float.floatToRawIntBits(r),
                "min(+0.0f, -0.0f) must be -0.0f");
    }

    public void testMaxF32SignedZero() {
        float r = max(GoKind.FLOAT32, f32(-0.0f), f32(+0.0f)).executeFloat(null);
        assertBitsEq(Float.floatToRawIntBits(+0.0f), Float.floatToRawIntBits(r),
                "max(-0.0f, +0.0f) must be +0.0f");
    }

    // ---- NaN propagation (positive + negative coverage) ----

    public void testMinF64NaNFirstArg() {
        double r = min(GoKind.FLOAT64, f64(Double.NaN), f64(1.5)).executeDouble(null);
        assertTrue(Double.isNaN(r), "min(NaN, 1.5) must be NaN");
    }

    public void testMinF64NaNSecondArg() {
        double r = min(GoKind.FLOAT64, f64(1.5), f64(Double.NaN)).executeDouble(null);
        assertTrue(Double.isNaN(r), "min(1.5, NaN) must be NaN");
    }

    public void testMaxF64NaNSecondArg() {
        double r = max(GoKind.FLOAT64, f64(2.5), f64(Double.NaN)).executeDouble(null);
        assertTrue(Double.isNaN(r), "max(2.5, NaN) must be NaN");
    }

    public void testMaxF64NaNBeatsNegZero() {
        // Negative coverage: the signed-zero tie rule must not outrank NaN.
        double r = max(GoKind.FLOAT64, f64(-0.0), f64(Double.NaN)).executeDouble(null);
        assertTrue(Double.isNaN(r), "max(-0.0, NaN) must be NaN, not +0.0");
    }

    public void testMinF64NaNBeatsNegZero() {
        double r = min(GoKind.FLOAT64, f64(Double.NaN), f64(-0.0)).executeDouble(null);
        assertTrue(Double.isNaN(r), "min(NaN, -0.0) must be NaN, not -0.0");
    }

    public void testMinF32NaN() {
        float r = min(GoKind.FLOAT32, f32(Float.NaN), f32(1.0f)).executeFloat(null);
        assertTrue(Float.isNaN(r), "min(NaN32, 1.0f) must be NaN");
    }

    // ---- infinities ----

    public void testMinF64Infinities() {
        double r = min(GoKind.FLOAT64, f64(Double.POSITIVE_INFINITY),
                f64(Double.NEGATIVE_INFINITY)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(Double.NEGATIVE_INFINITY),
                Double.doubleToRawLongBits(r), "min(+Inf, -Inf) must be -Inf");
    }

    public void testMaxF64Infinities() {
        double r = max(GoKind.FLOAT64, f64(Double.NEGATIVE_INFINITY),
                f64(Double.POSITIVE_INFINITY)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(Double.POSITIVE_INFINITY),
                Double.doubleToRawLongBits(r), "max(-Inf, +Inf) must be +Inf");
    }

    public void testMinF64PosInfVsPosZero() {
        double r = min(GoKind.FLOAT64, f64(Double.POSITIVE_INFINITY), f64(+0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(+0.0), Double.doubleToRawLongBits(r),
                "min(+Inf, +0.0) must be +0.0");
    }

    public void testMaxF64NegInfVsNegZero() {
        double r = max(GoKind.FLOAT64, f64(Double.NEGATIVE_INFINITY), f64(-0.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "max(-Inf, -0.0) must be -0.0");
    }

    // ---- ordinary float ordering still holds ----

    public void testMinF64Ordinary() {
        double r = min(GoKind.FLOAT64, f64(3.25), f64(1.5), f64(2.0)).executeDouble(null);
        assertBitsEq(Double.doubleToRawLongBits(1.5), Double.doubleToRawLongBits(r),
                "min(3.25, 1.5, 2.0) must be 1.5");
    }

    public void testMaxF32Ordinary() {
        float r = max(GoKind.FLOAT32, f32(1.0f), f32(3.5f), f32(2.0f)).executeFloat(null);
        assertBitsEq(Float.floatToRawIntBits(3.5f), Float.floatToRawIntBits(r),
                "max(1.0f, 3.5f, 2.0f) must be 3.5f");
    }

    // ---- untouched branches: ints and strings (regression pins) ----

    public void testMinInt64() {
        long r = min(GoKind.INT64, i64(3), i64(-7), i64(0)).executeLong(null);
        assertBitsEq(-7L, r, "min(3, -7, 0) must be -7");
    }

    public void testMaxUint64() {
        // 0xFFFFFFFFFFFFFFFF is the largest uint64; as signed long it is -1.
        long r = max(GoKind.UINT64, u64(-1L), u64(5L)).executeLong(null);
        assertBitsEq(-1L, r, "max(0xFFFF...F, 5) as uint64 must be 0xFFFF...F");
    }

    public void testMinString() {
        GoString r = (GoString) min(GoKind.STRING, str("b"), str("a")).executeObject(null);
        assertTrue("a".equals(r.toString()), "min(\"b\", \"a\") must be \"a\"");
    }

    public void testMaxString() {
        GoString r = (GoString) max(GoKind.STRING, str("a"), str("b")).executeObject(null);
        assertTrue("b".equals(r.toString()), "max(\"a\", \"b\") must be \"b\"");
    }
}
