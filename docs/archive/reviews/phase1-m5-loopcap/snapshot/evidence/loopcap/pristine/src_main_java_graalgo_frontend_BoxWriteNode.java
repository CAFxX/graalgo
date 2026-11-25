package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Writes a boxed (per-iteration capture, M6) variable.
 *
 * <p>The frame slot holds a {@link GoBox}; the write stores the canonical
 * boxed value (after Go's value-copy rule) in the cell. There is no
 * write-through: gc-oracle verified that a closure's write to a captured
 * iteration variable does not reach the loop-control variable (only the
 * post statement's read-modify-write observes the iteration cell and
 * writes the outer cell).
 */
public final class BoxWriteNode extends GoStmtNode {
    @Child private GoExprNode value;
    private final GoKind kind;
    private final int slot;

    public BoxWriteNode(GoKind kind, int slot, GoExprNode value) {
        this.kind = kind;
        this.slot = slot;
        this.value = value;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object v = Boxing.copyForAssign(kind, Boxing.box(kind, value, frame));
        GoBox cell = (GoBox) frame.getObject(slot);
        if (cell == null) {
            throw new IllegalStateException("BoxWriteNode: null cell in slot " + slot
                    + " (per-iteration cell not installed)");
        }
        cell.value = v;
    }
}
