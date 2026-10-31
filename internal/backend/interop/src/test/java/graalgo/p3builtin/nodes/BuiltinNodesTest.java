package graalgo.p3builtin.nodes;

import java.lang.reflect.Constructor;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for the {@code p3builtin} wire nodes (R-03 remediation, 2026-10-31).
 *
 * <p>Covers {@code min}/{@code max} (signed, unsigned, float NaN
 * propagation, strings), {@code clear} (slice zeroing, map clearing,
 * nil no-op, wrong-type failure), {@code runtime.NumCPU}, and
 * {@code runtime.GOMAXPROCS} (query/set/restore). Node constructors
 * are private, so the tests reach them by reflection, as in
 * {@code MathNodesTest}.
 *
 * <p>Not covered here, deliberately: {@code GoschedNode} and
 * {@code NumGoroutineNode} both call
 * {@code SchedulerCore.current()}, which throws
 * {@code IllegalStateException} unless a scheduler is installed by a
 * launcher — they cannot execute in this tree without one. Their
 * behaviour is covered differentially by programs that actually run
 * under the scheduler (e.g. the phase-2 channel corpus, which yields
 * and counts goroutines). Installing a throwaway scheduler in a unit
 * test would test the launcher's fixture, not the node.
 */
public final class BuiltinNodesTest {

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

    private static void assertEq(String e, String a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

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

    static final class ConstObj extends GoExprNode {
        private final Object v;

        ConstObj(GoKind kind, Object v) {
            super(kind);
            this.v = v;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return v;
        }
    }

    private static GoExprNode minMax(GoKind kind, boolean isMin, GoExprNode... args) throws Exception {
        Constructor<MinMaxNode> c = MinMaxNode.class.getDeclaredConstructor(
                GoKind.class, boolean.class, GoExprNode[].class);
        c.setAccessible(true);
        return c.newInstance(kind, isMin, args);
    }

    public void testMinMaxSignedInts() throws Exception {
        VirtualFrame f = frame();
        GoExprNode min = minMax(GoKind.INT64, true,
                new ConstLong(3), new ConstLong(1), new ConstLong(2));
        assertEq(1, min.executeLong(f), "min(3, 1, 2) must be 1");
        GoExprNode max = minMax(GoKind.INT64, false,
                new ConstLong(3), new ConstLong(1), new ConstLong(2));
        assertEq(3, max.executeLong(f), "max(3, 1, 2) must be 3");
        GoExprNode single = minMax(GoKind.INT64, true, new ConstLong(-7));
        assertEq(-7, single.executeLong(f), "min of one argument must return it");
    }

    public void testMinMaxUnsignedComparesUnsigned() throws Exception {
        VirtualFrame f = frame();
        // -1L as uint64 is 2^64-1, the largest value: signed comparison
        // would call it the smallest and fail both assertions.
        GoExprNode min = minMax(GoKind.UINT64, true, new ConstLong(-1L), new ConstLong(1L));
        assertEq(1, min.executeLong(f), "unsigned min(2^64-1, 1) must be 1");
        GoExprNode max = minMax(GoKind.UINT64, false, new ConstLong(-1L), new ConstLong(1L));
        assertEq(-1L, max.executeLong(f), "unsigned max(2^64-1, 1) must be 2^64-1");
    }

    public void testMinMaxFloatNanPropagates() throws Exception {
        VirtualFrame f = frame();
        GoExprNode min = minMax(GoKind.FLOAT64, true,
                new ConstDouble(Double.NaN), new ConstDouble(1.0));
        assertTrue(Double.isNaN(min.executeDouble(f)), "min(NaN, 1) must be NaN");
        GoExprNode max = minMax(GoKind.FLOAT64, false,
                new ConstDouble(1.0), new ConstDouble(Double.NaN));
        assertTrue(Double.isNaN(max.executeDouble(f)), "max(1, NaN) must be NaN");
        GoExprNode plain = minMax(GoKind.FLOAT64, true,
                new ConstDouble(2.5), new ConstDouble(0.5));
        assertEq(0.5, plain.executeDouble(f), "min(2.5, 0.5) must be 0.5");
    }

    public void testMinMaxStringsAreBytewise() throws Exception {
        VirtualFrame f = frame();
        GoExprNode min = minMax(GoKind.STRING, true,
                new ConstObj(GoKind.STRING, GoString.fromUtf8("banana")),
                new ConstObj(GoKind.STRING, GoString.fromUtf8("apple")));
        Object got = min.executeObject(f);
        assertTrue(got instanceof GoString, "string min must return a GoString");
        assertEq("apple", new String(((GoString) got).bytes(),
                java.nio.charset.StandardCharsets.UTF_8), "min(banana, apple) must be apple");
    }

    public void testClearSliceZeroesInPlace() throws Exception {
        VirtualFrame f = frame();
        byte[] backing = new byte[]{1, 2, 3, 4};
        GoSlice s = new GoSlice(backing, 0, 4, 4);
        Constructor<ClearNode> c = ClearNode.class.getDeclaredConstructor(GoExprNode.class);
        c.setAccessible(true);
        GoExprNode clear = c.newInstance(new ConstObj(GoKind.SLICE, s));
        assertEq(0, clear.executeLong(f), "clear in statement position returns 0");
        assertTrue(backing[0] == 0 && backing[1] == 0 && backing[2] == 0 && backing[3] == 0,
                "clear must zero the shared backing array, not a copy");
    }

    public void testClearMapAndNilAndWrongType() throws Exception {
        VirtualFrame f = frame();
        GoMap m = new GoMap();
        m.put("k", "v");
        Constructor<ClearNode> c = ClearNode.class.getDeclaredConstructor(GoExprNode.class);
        c.setAccessible(true);
        GoExprNode clearMap = c.newInstance(new ConstObj(GoKind.MAP, m));
        clearMap.executeObject(f);
        assertEq(0, m.size(), "clear(map) must empty the map");
        GoExprNode clearNil = c.newInstance(new ConstObj(GoKind.SLICE, null));
        assertTrue(clearNil.executeObject(f) == null, "clear(nil) must be a no-op returning null");
        GoExprNode clearBad = c.newInstance(new ConstObj(GoKind.STRING,
                GoString.fromUtf8("x")));
        try {
            clearBad.executeObject(f);
            throw new AssertionError("clear of a string must throw");
        } catch (IllegalStateException expected) {
            // Expected.
        }
    }

    public void testNumCpuMatchesHost() throws Exception {
        VirtualFrame f = frame();
        Constructor<NumCPUNode> c = NumCPUNode.class.getDeclaredConstructor(GoKind.class);
        c.setAccessible(true);
        GoExprNode n = c.newInstance(GoKind.INT64);
        long got = n.executeLong(f);
        assertTrue(got > 0, "NumCPU must be positive, got " + got);
        assertEq(Runtime.getRuntime().availableProcessors(), got,
                "NumCPU must be the host processor count");
    }

    public void testGoMaxProcsQuerySetRestore() throws Exception {
        VirtualFrame f = frame();
        Constructor<GoMaxProcsNode> c = GoMaxProcsNode.class.getDeclaredConstructor(
                GoKind.class, GoExprNode.class);
        c.setAccessible(true);
        GoExprNode query = c.newInstance(GoKind.INT64, new ConstLong(0));
        long saved = query.executeLong(f);
        assertTrue(saved > 0, "initial GOMAXPROCS must be positive, got " + saved);
        try {
            GoExprNode set = c.newInstance(GoKind.INT64, new ConstLong(3));
            assertEq(saved, set.executeLong(f), "GOMAXPROCS(3) must return the previous value");
            assertEq(3, query.executeLong(f), "GOMAXPROCS(0) must now report 3");
            GoExprNode setNeg = c.newInstance(GoKind.INT64, new ConstLong(-1));
            assertEq(3, setNeg.executeLong(f),
                    "GOMAXPROCS(-1) must query without changing, per gc");
        } finally {
            GoExprNode restore = c.newInstance(GoKind.INT64, new ConstLong((int) saved));
            restore.executeLong(f);
            assertEq(saved, query.executeLong(f), "test must restore GOMAXPROCS");
        }
    }
}
