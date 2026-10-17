package graalgo.p3builtin.nodes;

import java.nio.charset.StandardCharsets;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * Pins {@code min}/{@code max} for every int carrier (P1-I5: the node had
 * no {@code executeInt}, so {@code x := min(int32a, int32b)} crashed — the
 * base implementation throws UOE — on every consumer that dispatches on
 * the frame slot kind: AssignNode, VarDeclNode, ExprStmtNode, ...).
 *
 * <p>RED (pre-fix): every {@code test*} below that calls
 * {@code executeInt} fails with {@code UnsupportedOperationException}.
 * GREEN: 8/16/32-bit int/uint min/max fold correctly (unsigned kinds use
 * unsigned comparison), and {@code executeLong} on sub-64-bit kinds
 * delegates with D5 sign/zero extension (the AtomicOpNode int32
 * precedent).
 *
 * <p>Style follows the module's red-green runner (public no-arg
 * {@code test*} methods, {@link AssertionError} on failure, registered
 * in {@code graalgo.iface.IfaceTestRunner}).
 */
public final class MinMaxNodeTest {

    /** Int-carrier constant honoring D5 sign/zero extension. */
    private static final class ConstInt extends GoExprNode {
        private final int v;

        ConstInt(GoKind kind, int v) {
            super(kind);
            this.v = v;
        }

        @Override
        public int executeInt(VirtualFrame frame) {
            return v;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return kind().isUintKind() ? (v & 0xFFFFFFFFL) : v;
        }
    }

    private static final class ConstLong extends GoExprNode {
        private final long v;

        ConstLong(GoKind kind, long v) {
            super(kind);
            this.v = v;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return v;
        }
    }

    private static final class ConstFloat extends GoExprNode {
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

    private static MinMaxNode min(GoKind kind, GoExprNode... args) {
        return new MinMaxNode(kind, true, args);
    }

    private static MinMaxNode max(GoKind kind, GoExprNode... args) {
        return new MinMaxNode(kind, false, args);
    }

    // --- P1-I5 regression: executeInt on sub-64-bit kinds ---

    public void testMinInt32() {
        int r = min(GoKind.INT32, new ConstInt(GoKind.INT32, 5),
                new ConstInt(GoKind.INT32, 3), new ConstInt(GoKind.INT32, 8))
                .executeInt(null);
        if (r != 3) {
            throw new AssertionError("min(int32 5,3,8) = " + r + ", want 3");
        }
    }

    public void testMaxInt32() {
        int r = max(GoKind.INT32, new ConstInt(GoKind.INT32, 5),
                new ConstInt(GoKind.INT32, 3), new ConstInt(GoKind.INT32, 8))
                .executeInt(null);
        if (r != 8) {
            throw new AssertionError("max(int32 5,3,8) = " + r + ", want 8");
        }
    }

    public void testMinInt32Negative() {
        int r = min(GoKind.INT32, new ConstInt(GoKind.INT32, -5),
                new ConstInt(GoKind.INT32, 3)).executeInt(null);
        if (r != -5) {
            throw new AssertionError("min(int32 -5,3) = " + r + ", want -5");
        }
    }

    public void testUint32IsUnsigned() {
        // 0xFFFFFFFF > 1 as unsigned; signed comparison would say -1 < 1.
        int min = min(GoKind.UINT32, new ConstInt(GoKind.UINT32, -1),
                new ConstInt(GoKind.UINT32, 1)).executeInt(null);
        if (min != 1) {
            throw new AssertionError("min(uint32 max,1) = " + min + ", want 1");
        }
        int max = max(GoKind.UINT32, new ConstInt(GoKind.UINT32, -1),
                new ConstInt(GoKind.UINT32, 1)).executeInt(null);
        if (max != -1) { // bit pattern 0xFFFFFFFF
            throw new AssertionError("max(uint32 max,1) = " + max + ", want 0xFFFFFFFF");
        }
    }

    public void testNarrowKinds() {
        int r8 = min(GoKind.INT8, new ConstInt(GoKind.INT8, -5),
                new ConstInt(GoKind.INT8, 3)).executeInt(null);
        if (r8 != -5) {
            throw new AssertionError("min(int8 -5,3) = " + r8);
        }
        int r16 = max(GoKind.INT16, new ConstInt(GoKind.INT16, 300),
                new ConstInt(GoKind.INT16, -400)).executeInt(null);
        if (r16 != 300) {
            throw new AssertionError("max(int16 300,-400) = " + r16);
        }
        int ru16 = min(GoKind.UINT16, new ConstInt(GoKind.UINT16, 60000),
                new ConstInt(GoKind.UINT16, 100)).executeInt(null);
        if (ru16 != 100) {
            throw new AssertionError("min(uint16 60000,100) = " + ru16);
        }
    }

    public void testSingleArg() {
        int r = min(GoKind.INT32, new ConstInt(GoKind.INT32, 42)).executeInt(null);
        if (r != 42) {
            throw new AssertionError("min(int32 42) = " + r);
        }
    }

    // --- executeLong delegation with D5 extension (AtomicOpNode precedent) ---

    public void testExecuteLongOnInt32() {
        long r = min(GoKind.INT32, new ConstInt(GoKind.INT32, 5),
                new ConstInt(GoKind.INT32, 3)).executeLong(null);
        if (r != 3L) {
            throw new AssertionError("min(int32).executeLong = " + r);
        }
    }

    public void testExecuteLongOnUint32ZeroExtends() {
        long r = max(GoKind.UINT32, new ConstInt(GoKind.UINT32, -1),
                new ConstInt(GoKind.UINT32, 1)).executeLong(null);
        if (r != 0xFFFFFFFFL) {
            throw new AssertionError("max(uint32).executeLong = " + r + ", want 4294967295");
        }
    }

    public void testExecuteLongOnInt64Unchanged() {
        long r = min(GoKind.INT64, new ConstLong(GoKind.INT64, 5L),
                new ConstLong(GoKind.INT64, 3L)).executeLong(null);
        if (r != 3L) {
            throw new AssertionError("min(int64).executeLong = " + r);
        }
        long ru = max(GoKind.UINT64, new ConstLong(GoKind.UINT64, -1L),
                new ConstLong(GoKind.UINT64, 1L)).executeLong(null);
        if (Long.compareUnsigned(ru, 1L) <= 0) {
            throw new AssertionError("max(uint64).executeLong = " + ru);
        }
    }

    // --- Other carriers unaffected ---

    public void testFloat32NanPropagates() {
        float r = new MinMaxNode(GoKind.FLOAT32, true,
                new GoExprNode[] { new ConstFloat(1.5f), new ConstFloat(Float.NaN) })
                .executeFloat(null);
        if (!Float.isNaN(r)) {
            throw new AssertionError("min(float32 1.5,NaN) = " + r + ", want NaN");
        }
    }

    public void testStringMin() {
        GoExprNode a = str("b");
        GoExprNode b = str("a");
        Object r = new MinMaxNode(GoKind.STRING, true, new GoExprNode[] { a, b })
                .executeObject(null);
        if (!"a".equals(r.toString())) {
            throw new AssertionError("min(\"b\",\"a\") = " + r);
        }
    }

    private static GoExprNode str(String s) {
        final GoString gs = new GoString(s.getBytes(StandardCharsets.UTF_8));
        return new GoExprNode(GoKind.STRING) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return gs;
            }
        };
    }
}
