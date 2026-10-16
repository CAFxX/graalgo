package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

/**
 * The shared {@code clear} semantic matrix (P1-2, 2026-10-16).
 *
 * <p>The truffle {@link ClearNode} (live only in {@code NodeFactoryV2})
 * duplicates the interop {@code graalgo.p3builtin.nodes.ClearNode} (live on
 * the v1 wire path). The two MUST stay semantically identical. This class
 * pins the matrix on the truffle node's path, in-JVM; the differential
 * corpus program {@code test/differential/corpus/50_clear_builtin} pins the
 * same matrix on the interop path against gc. Matrix:
 *
 * <ul>
 *   <li>nil map / nil slice: no-op, no exception;</li>
 *   <li>non-nil map with entries: all entries deleted;</li>
 *   <li>slice of every backing-array type ({@code byte[]}, {@code short[]},
 *       {@code int[]}, {@code long[]}, {@code float[]}, {@code double[]},
 *       {@code char[]}, {@code boolean[]}, {@code Object[]}): the
 *       {@code len} elements are zeroed;</li>
 *   <li>only the {@code len} elements are zeroed; backing beyond
 *       {@code offset+len} and before {@code offset} is untouched.</li>
 * </ul>
 *
 * <p>RED (pre-fix): {@code testFloatBacking} and {@code testCharBacking}
 * fail — the node silently skipped {@code float[]}/{@code char[]} backings
 * (a latent divergence from the interop ClearNode); {@code testNilSliceNoOp}
 * fails with an NPE — a nil slice (a nil GoSlice reference) crashed on
 * {@code gs.array()}. GREEN: all backings zeroed, nil is a no-op, matching
 * gc and the interop ClearNode.
 */
public final class ClearNodeSemanticsTest {

    /** A GoExprNode evaluating to a constant target of a fixed kind. */
    static final class ConstTarget extends GoExprNode {
        private final Object value;

        ConstTarget(Object value, GoKind kind) {
            super(kind);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0], new FrameDescriptor());
    }

    private static void clear(Object target, GoKind kind) {
        ClearNode node = new ClearNode(new ConstTarget(target, kind));
        long r = node.executeLong(frame());
        if (r != 0L) {
            throw new AssertionError("clear must return 0, got " + r);
        }
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testNilMapNoOp() {
        clear(null, GoKind.MAP); // must not throw
    }

    public void testNilSliceNoOp() {
        // A nil slice is a nil GoSlice reference (the GoSlice constructor
        // rejects null backing arrays): clear(nil) must be a no-op.
        clear(null, GoKind.SLICE); // must not throw
    }

    public void testMapCleared() {
        GoMap m = new GoMap();
        m.put("a", 1);
        m.put("b", 2);
        clear(m, GoKind.MAP);
        assertTrue(m.size() == 0, "map not cleared, size=" + m.size());
    }

    public void testByteBacking() {
        byte[] a = {1, 2, 3};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == 0 && a[1] == 0 && a[2] == 0, "byte[] not zeroed");
    }

    public void testShortBacking() {
        short[] a = {1, 2, 3};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == 0 && a[1] == 0 && a[2] == 0, "short[] not zeroed");
    }

    public void testIntBacking() {
        int[] a = {1, 2, 3};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == 0 && a[1] == 0 && a[2] == 0, "int[] not zeroed");
    }

    public void testLongBacking() {
        long[] a = {1, 2, 3};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == 0 && a[1] == 0 && a[2] == 0, "long[] not zeroed");
    }

    public void testFloatBacking() {
        float[] a = {1.5f, -2.5f, 3.25f};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == 0.0f && a[1] == 0.0f && a[2] == 0.0f,
                "float[] not zeroed: " + a[0] + "," + a[1] + "," + a[2]);
    }

    public void testDoubleBacking() {
        double[] a = {1.5, -2.5, 3.25};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == 0.0 && a[1] == 0.0 && a[2] == 0.0, "double[] not zeroed");
    }

    public void testCharBacking() {
        char[] a = {'a', 'b', 'c'};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == '\0' && a[1] == '\0' && a[2] == '\0', "char[] not zeroed");
    }

    public void testBooleanBacking() {
        boolean[] a = {true, false, true};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(!a[0] && !a[1] && !a[2], "boolean[] not zeroed");
    }

    public void testObjectBacking() {
        Object[] a = {"a", "b", "c"};
        clear(new GoSlice(a, 0, 3, 3), GoKind.SLICE);
        assertTrue(a[0] == null && a[1] == null && a[2] == null, "Object[] not nulled");
    }

    public void testOnlyLenElementsZeroed() {
        int[] a = {7, 7, 7, 7, 7};
        // offset 1, len 2: zeroes a[1], a[2]; a[0], a[3], a[4] untouched.
        clear(new GoSlice(a, 1, 2, 4), GoKind.SLICE);
        assertTrue(a[0] == 7 && a[1] == 0 && a[2] == 0 && a[3] == 7 && a[4] == 7,
                "offset/len zeroing wrong: " + a[0] + "," + a[1] + "," + a[2] + "," + a[3] + "," + a[4]);
    }

    public void testEmptySliceNoOp() {
        int[] a = {7, 7};
        clear(new GoSlice(a, 0, 0, 2), GoKind.SLICE);
        assertTrue(a[0] == 7 && a[1] == 7, "empty slice clear touched backing");
    }
}
