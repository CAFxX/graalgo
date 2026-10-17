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
            // clear on a nil slice is a no-op (gc parity; a nil slice is a
            // null GoSlice reference per GoSlice's contract).
            if (target != null) {
                // Zero the slice's elements via the single D5-carrier-aware
                // implementation shared with the p3builtin ClearNode
                // (GoSlice.clearElements): covers float[] (R2 P0-2), and
                // zeroes complex (0+0i, never null) and struct (fresh zero,
                // never null) backings (R2 P1-9).
                ((GoSlice) target).clearElements();
            }
        }
        return 0L;
    }
}
