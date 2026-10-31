package graalgo.p3math.nodes;

import java.lang.reflect.Constructor;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;

/**
 * Unit tests for the {@code p3math} wire nodes (R-03 remediation, 2026-10-31).
 *
 * <p>These nodes were previously covered only by differential corpus
 * runs. The tests execute the nodes directly (the same pattern as
 * {@code IfaceDispatchNodeTest}: constant operand nodes + a fresh
 * Truffle frame). Node constructors are private, so the tests reach
 * them by reflection — the wiring under test is the execute method,
 * not the constructor's visibility.
 *
 * <p>Expectations are gc-known constants (e.g. {@code math.Sqrt(2)}
 * bit pattern), raw-bit edge cases that distinguish correct rounding
 * from approximate results ({@code -0.0} bit patterns, NaN payload
 * preservation in the bits nodes), and operand-order cases for FMA
 * where swapping operands changes the result.
 */
public final class MathNodesTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static void assertEq(double e, double a, String msg) {
        if (Double.compare(e, a) != 0) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static void assertEq(float e, float a, String msg) {
        if (Float.compare(e, a) != 0) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    /** Constant double operand. */
    static final class ConstDouble extends GoExprNode {
        private final double v;

        ConstDouble(double v) {
            super(GoKind.FLOAT64);
            this.v = v;
        }

        @Override
        public double executeDouble(VirtualFrame frame) {
            return v;
        }
    }

    /** Constant long operand. */
    static final class ConstLong extends GoExprNode {
        private final long v;

        ConstLong(long v) {
            super(GoKind.INT64);
            this.v = v;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return v;
        }
    }

    /** Constant int operand. */
    static final class ConstInt extends GoExprNode {
        private final int v;

        ConstInt(int v) {
            super(GoKind.INT32);
            this.v = v;
        }

        @Override
        public int executeInt(VirtualFrame frame) {
            return v;
        }
    }

    /** Constant float operand. */
    static final class ConstFloat extends GoExprNode {
        private final float v;

        ConstFloat(float v) {
            super(GoKind.FLOAT32);
            this.v = v;
        }

        @Override
        public float executeFloat(VirtualFrame frame) {
            return v;
        }
    }

    @SuppressWarnings("unchecked")
    private static <T extends GoExprNode> T node(Class<T> cls, Object... args) throws Exception {
        Class<?>[] types = new Class<?>[args.length];
        for (int i = 0; i < args.length; i++) {
            types[i] = GoExprNode.class;
        }
        Constructor<T> c = cls.getDeclaredConstructor(types);
        c.setAccessible(true);
        return c.newInstance(args);
    }

    public void testSqrtKnownValues() throws Exception {
        VirtualFrame f = frame();
        // gc math.Sqrt(2) = 1.4142135623730951, bits 0x3FF6A09E667F3BCD.
        GoExprNode n = node(MathSqrtNode.class, new ConstDouble(2.0));
        assertEq(0x3FF6A09E667F3BCDL, Double.doubleToRawLongBits(n.executeDouble(f)),
                "Sqrt(2) must be correctly rounded to the gc value");
        assertEq(2.0, node(MathSqrtNode.class, new ConstDouble(4.0)).executeDouble(f),
                "Sqrt(4) must be 2");
        assertEq(0.5, node(MathSqrtNode.class, new ConstDouble(0.25)).executeDouble(f),
                "Sqrt(0.25) must be 0.5");
        assertEq(0.0, node(MathSqrtNode.class, new ConstDouble(0.0)).executeDouble(f),
                "Sqrt(0) must be 0");
        assertTrue(Double.isNaN(node(MathSqrtNode.class, new ConstDouble(-1.0)).executeDouble(f)),
                "Sqrt(-1) must be NaN");
        assertEq(Double.POSITIVE_INFINITY,
                node(MathSqrtNode.class, new ConstDouble(Double.POSITIVE_INFINITY)).executeDouble(f),
                "Sqrt(+Inf) must be +Inf");
    }

    public void testFloat64BitsRoundtripAndRawEdges() throws Exception {
        VirtualFrame f = frame();
        GoExprNode bits = node(MathF64BitsNode.class, new ConstDouble(1.5));
        assertEq(0x3FF8000000000000L, bits.executeLong(f), "Float64bits(1.5)");
        GoExprNode negZero = node(MathF64BitsNode.class, new ConstDouble(-0.0));
        assertEq(0x8000000000000000L, negZero.executeLong(f),
                "Float64bits(-0.0) must preserve the sign bit (raw bits)");
        long nanPayload = 0x7FF8000000000001L;
        GoExprNode nanBits = node(MathF64BitsNode.class,
                new ConstDouble(Double.longBitsToDouble(nanPayload)));
        assertEq(nanPayload, nanBits.executeLong(f),
                "Float64bits must preserve NaN payloads (raw, not canonicalized)");
        GoExprNode from = node(MathF64FromBitsNode.class, new ConstLong(0x3FF8000000000000L));
        assertEq(1.5, from.executeDouble(f), "Float64frombits(1.5 bits)");
        GoExprNode fromNegZero = node(MathF64FromBitsNode.class,
                new ConstLong(0x8000000000000000L));
        assertEq(0x8000000000000000L,
                Double.doubleToRawLongBits(fromNegZero.executeDouble(f)),
                "Float64frombits must restore -0.0");
    }

    public void testFloat32BitsRoundtripAndRawEdges() throws Exception {
        VirtualFrame f = frame();
        GoExprNode bits = node(MathF32BitsNode.class, new ConstFloat(1.5f));
        assertEq(0x3FC00000, bits.executeInt(f), "Float32bits(1.5)");
        GoExprNode negZero = node(MathF32BitsNode.class, new ConstFloat(-0.0f));
        assertEq(0x80000000, negZero.executeInt(f),
                "Float32bits(-0.0) must preserve the sign bit");
        GoExprNode from = node(MathF32FromBitsNode.class, new ConstInt(0x3FC00000));
        assertEq(1.5f, from.executeFloat(f), "Float32frombits(1.5 bits)");
    }

    public void testFmaOperandOrderAndSpecials() throws Exception {
        VirtualFrame f = frame();
        // Wiring check: swapping x and z changes the result (16 vs 32),
        // so a miswired node fails even though fma itself is commutative
        // in its first two arguments.
        GoExprNode a = node(MathFmaNode.class,
                new ConstDouble(2.0), new ConstDouble(3.0), new ConstDouble(10.0));
        assertEq(16.0, a.executeDouble(f), "FMA(2, 3, 10) must be 16");
        GoExprNode b = node(MathFmaNode.class,
                new ConstDouble(10.0), new ConstDouble(3.0), new ConstDouble(2.0));
        assertEq(32.0, b.executeDouble(f), "FMA(10, 3, 2) must be 32");
        GoExprNode nan = node(MathFmaNode.class,
                new ConstDouble(Double.NaN), new ConstDouble(1.0), new ConstDouble(1.0));
        assertTrue(Double.isNaN(nan.executeDouble(f)), "FMA with NaN must be NaN");
        GoExprNode simple = node(MathFmaNode.class,
                new ConstDouble(1.0), new ConstDouble(1.0), new ConstDouble(1.0));
        assertEq(2.0, simple.executeDouble(f), "FMA(1, 1, 1) must be 2");
    }
}
