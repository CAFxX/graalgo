package graalgo.p3builtin.nodes;

import java.nio.charset.StandardCharsets;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Node-level pins for the p3 {@code "clear"} wire node (I-8): the element
 * kind resolved at build time must reach the slice zeroing, so
 * {@code Object[]} slots receive the Go zero value instead of Java
 * {@code null}. The full carrier matrix lives in the truffle module's
 * {@code ClearSliceTest} (same canonical {@code Boxing.clearSlice}).
 *
 * <p>Style follows the module's red-green runner (public no-arg
 * {@code test*} methods, {@link AssertionError} on failure, registered
 * in {@code graalgo.iface.IfaceTestRunner}).
 */
public final class ClearNodeTest {

    private static final class ConstSlice extends GoExprNode {
        private final GoSlice s;

        ConstSlice(GoSlice s) {
            super(GoKind.SLICE);
            this.s = s;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return s;
        }
    }

    public void testClearStringSliceWritesEmpty() {
        Object[] arr = new Object[] {
                new GoString("hi".getBytes(StandardCharsets.UTF_8)) };
        GoSlice s = new GoSlice(arr, 0, 1, 1);
        new ClearNode(new ConstSlice(s), GoKind.STRING, null).executeLong(null);
        if (arr[0] != GoString.EMPTY) {
            throw new AssertionError("string slot not EMPTY after clear: " + arr[0]);
        }
    }

    public void testClearInterfaceSliceNulls() {
        Object[] arr = new Object[] { Integer.valueOf(7) };
        GoSlice s = new GoSlice(arr, 0, 1, 1);
        new ClearNode(new ConstSlice(s), GoKind.INTERFACE, null).executeLong(null);
        if (arr[0] != null) {
            throw new AssertionError("interface slot not nulled after clear: " + arr[0]);
        }
    }

    public void testClearFloat32Slice() {
        float[] arr = new float[] { 1.5f };
        GoSlice s = new GoSlice(arr, 0, 1, 1);
        // The p3 node already handled float.class before the I-8 rework;
        // pin that the rewrite did not regress it.
        new ClearNode(new ConstSlice(s), GoKind.FLOAT32, null).executeLong(null);
        if (arr[0] != 0.0f) {
            throw new AssertionError("float32 slot not zeroed after clear: " + arr[0]);
        }
    }
}
