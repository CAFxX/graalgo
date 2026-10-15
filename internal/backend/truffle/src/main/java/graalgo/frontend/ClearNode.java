package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

// verifies: GG-0021 (WS G builtin)
/**
 * The {@code clear} builtin (WS G; verifies GG-0021).
 *
 * <p>{@code clear(m)} deletes all entries from map {@code m}.
 * {@code clear(s)} zeroes all elements of slice {@code s} (up to its length).
 * It is a statement-only builtin (no result): {@link #executeLong} is
 * implemented so {@link ExprStmtNode} can execute it.
 */
public final class ClearNode extends GoExprNode {
    @Child private GoExprNode targetChild;

    public ClearNode(GoExprNode targetChild) {
        // No result; INT64 is a placeholder kind (only executeLong is
        // ever called, via ExprStmtNode).
        super(GoKind.INT64);
        this.targetChild = targetChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object target = targetChild.executeObject(frame);
        GoKind k = targetChild.kind();
        if (k == GoKind.MAP) {
            // clear on a nil map is a no-op (gc parity).
            if (target != null) {
                ((GoMap) target).clear();
            }
        } else if (k == GoKind.SLICE) {
            GoSlice gs = (GoSlice) target;
            Object arr = gs.array();
            int off = gs.offset();
            int len = gs.len();
            // Zero the slice's elements. Backing arrays are typed
            // (byte[], short[], int[], long[], double[], Object[], etc. per D5).
            if (arr instanceof byte[] ba) {
                java.util.Arrays.fill(ba, off, off + len, (byte) 0);
            } else if (arr instanceof short[] sa) {
                java.util.Arrays.fill(sa, off, off + len, (short) 0);
            } else if (arr instanceof int[] ia) {
                java.util.Arrays.fill(ia, off, off + len, 0);
            } else if (arr instanceof long[] la) {
                java.util.Arrays.fill(la, off, off + len, 0L);
            } else if (arr instanceof double[] da) {
                java.util.Arrays.fill(da, off, off + len, 0.0);
            } else if (arr instanceof boolean[] bo) {
                java.util.Arrays.fill(bo, off, off + len, false);
            } else if (arr instanceof Object[] oa) {
                java.util.Arrays.fill(oa, off, off + len, null);
            }
            // clear on a nil slice is a no-op (arr null handled by instanceof).
        }
        return 0L;
    }
}
