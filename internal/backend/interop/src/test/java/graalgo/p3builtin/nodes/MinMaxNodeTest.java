package graalgo.p3builtin.nodes;

import java.lang.reflect.Constructor;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.ConstNode;
import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * Unit tests for the {@code "minmax"} wire node ({@link MinMaxNode}) on the
 * sync-run path.
 *
 * <p>verifies: REVIEW-2026-11-04-11 NEW-P1-19 (R11-MINMAX): the float folds
 * must implement gc's signed-zero tie rule — {@code min} prefers
 * {@code -0.0}, {@code max} prefers {@code +0.0} — in both argument
 * orders, for float32 and float64. The previous {@code v < best} /
 * {@code v > best} fold treated {@code -0.0 == +0.0} and kept whichever
 * zero arrived first, which is bit-observable via {@code math.Signbit} /
 * {@code math.Float64bits}. NaN propagation must still outrank the tie
 * rule, and the int/string/single-argument paths must not regress.
 *
 * <p>Construction uses reflection because {@code MinMaxNode}'s constructor
 * is private (nodes are normally built via {@code MinMaxNode.build} from
 * wire JSON); the constant arguments are {@link ConstNode}s, so execution
 * needs only a plain {@link VirtualFrame}.
 */
public final class MinMaxNodeTest {

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

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
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

    // ---- signed-zero tie rule, float64, both orders ----

    public void testMinF64PosZeroThenNegZero() {
        double r = min(GoKind.FLOAT64, f64(+0.0), f64(-0.0)).executeDouble(frame());
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(+0.0, -0.0) must be -0.0");
    }

    public void testMinF64NegZeroThenPosZero() {
        double r = min(GoKind.FLOAT64, f64(-0.0), f64(+0.0)).executeDouble(frame());
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(-0.0, +0.0) must be -0.0");
    }

    public void testMaxF64NegZeroThenPosZero() {
        double r = max(GoKind.FLOAT64, f64(-0.0), f64(+0.0)).executeDouble(frame());
        assertBitsEq(Double.doubleToRawLongBits(+0.0), Double.doubleToRawLongBits(r),
                "max(-0.0, +0.0) must be +0.0");
    }

    public void testMaxF64PosZeroThenNegZero() {
        double r = max(GoKind.FLOAT64, f64(+0.0), f64(-0.0)).executeDouble(frame());
        assertBitsEq(Double.doubleToRawLongBits(+0.0), Double.doubleToRawLongBits(r),
                "max(+0.0, -0.0) must be +0.0");
    }

    // ---- signed-zero tie rule, float32, both orders ----

    public void testMinF32PosZeroThenNegZero() {
        float r = min(GoKind.FLOAT32, f32(+0.0f), f32(-0.0f)).executeFloat(frame());
        assertBitsEq(Float.floatToRawIntBits(-0.0f), Float.floatToRawIntBits(r),
                "min(+0.0f, -0.0f) must be -0.0f");
    }

    public void testMinF32NegZeroThenPosZero() {
        float r = min(GoKind.FLOAT32, f32(-0.0f), f32(+0.0f)).executeFloat(frame());
        assertBitsEq(Float.floatToRawIntBits(-0.0f), Float.floatToRawIntBits(r),
                "min(-0.0f, +0.0f) must be -0.0f");
    }

    public void testMaxF32NegZeroThenPosZero() {
        float r = max(GoKind.FLOAT32, f32(-0.0f), f32(+0.0f)).executeFloat(frame());
        assertBitsEq(Float.floatToRawIntBits(+0.0f), Float.floatToRawIntBits(r),
                "max(-0.0f, +0.0f) must be +0.0f");
    }

    public void testMaxF32PosZeroThenNegZero() {
        float r = max(GoKind.FLOAT32, f32(+0.0f), f32(-0.0f)).executeFloat(frame());
        assertBitsEq(Float.floatToRawIntBits(+0.0f), Float.floatToRawIntBits(r),
                "max(+0.0f, -0.0f) must be +0.0f");
    }

    // ---- multi-argument zero mixes ----

    public void testMinF64ThreeArgsZeroMix() {
        double r = min(GoKind.FLOAT64, f64(+0.0), f64(-0.0), f64(-0.0)).executeDouble(frame());
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(+0.0, -0.0, -0.0) must be -0.0");
    }

    public void testMaxF64ThreeArgsZeroMix() {
        double r = max(GoKind.FLOAT64, f64(-0.0), f64(+0.0), f64(+0.0)).executeDouble(frame());
        assertBitsEq(Double.doubleToRawLongBits(+0.0), Double.doubleToRawLongBits(r),
                "max(-0.0, +0.0, +0.0) must be +0.0");
    }

    public void testMinF32ThreeArgsZeroMix() {
        float r = min(GoKind.FLOAT32, f32(+0.0f), f32(+0.0f), f32(-0.0f)).executeFloat(frame());
        assertBitsEq(Float.floatToRawIntBits(-0.0f), Float.floatToRawIntBits(r),
                "min(+0.0f, +0.0f, -0.0f) must be -0.0f");
    }

    // ---- NaN still outranks the tie rule; infinities; ordinary order ----

    public void testMinF64NanBeatsZero() {
        double r = min(GoKind.FLOAT64, f64(Double.NaN), f64(-0.0)).executeDouble(frame());
        assertTrue(Double.isNaN(r), "min(NaN, -0.0) must be NaN");
    }

    public void testMaxF64NanBeatsZero() {
        double r = max(GoKind.FLOAT64, f64(+0.0), f64(Double.NaN)).executeDouble(frame());
        assertTrue(Double.isNaN(r), "max(+0.0, NaN) must be NaN");
    }

    public void testMinMaxF64Infinities() {
        double lo = min(GoKind.FLOAT64, f64(Double.POSITIVE_INFINITY),
                f64(Double.NEGATIVE_INFINITY)).executeDouble(frame());
        assertTrue(lo == Double.NEGATIVE_INFINITY, "min(+Inf, -Inf) must be -Inf");
        double hi = max(GoKind.FLOAT64, f64(Double.POSITIVE_INFINITY),
                f64(Double.NEGATIVE_INFINITY)).executeDouble(frame());
        assertTrue(hi == Double.POSITIVE_INFINITY, "max(+Inf, -Inf) must be +Inf");
    }

    public void testMinMaxF64Ordinary() {
        double lo = min(GoKind.FLOAT64, f64(3.5), f64(-2.5)).executeDouble(frame());
        assertTrue(lo == -2.5, "min(3.5, -2.5) must be -2.5");
        double hi = max(GoKind.FLOAT64, f64(3.5), f64(-2.5)).executeDouble(frame());
        assertTrue(hi == 3.5, "max(3.5, -2.5) must be 3.5");
    }

    // ---- untouched paths: single argument, ints, strings ----

    public void testSingleArgPreservesNegZero() {
        double r = min(GoKind.FLOAT64, f64(-0.0)).executeDouble(frame());
        assertBitsEq(Double.doubleToRawLongBits(-0.0), Double.doubleToRawLongBits(r),
                "min(-0.0) must be -0.0");
    }

    public void testMinMaxInt() {
        long lo = min(GoKind.INT64, i64(3), i64(-7)).executeLong(frame());
        assertTrue(lo == -7, "min(3, -7) must be -7");
        long hi = max(GoKind.INT64, i64(3), i64(-7)).executeLong(frame());
        assertTrue(hi == 3, "max(3, -7) must be 3");
    }

    public void testMaxUint64UsesUnsignedCompare() {
        long hi = max(GoKind.UINT64, u64(-1L), u64(1L)).executeLong(frame());
        assertTrue(hi == -1L, "max(uint64 max, 1) must be uint64 max");
    }

    public void testMinMaxString() {
        Object lo = min(GoKind.STRING, str("b"), str("a")).executeObject(frame());
        assertTrue(GoString.fromUtf8("a").equals(lo), "min(\"b\", \"a\") must be \"a\"");
        Object hi = max(GoKind.STRING, str("b"), str("a")).executeObject(frame());
        assertTrue(GoString.fromUtf8("b").equals(hi), "max(\"b\", \"a\") must be \"b\"");
    }
}
