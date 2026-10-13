package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * Writes a boxed (per-iteration capture) variable.
 *
 * <p>The frame slot {@code cellSlot} holds a {@link GoBox}; the write
 * stores the canonical boxed value (after Go's value-copy rule) in the
 * cell. The cell is fully detached: propagation to the carry cell of a
 * {@code for}-header variable happens only through the explicit
 * end-of-iteration {@link BoxWritebackNode}, matching gc's
 * {@code loopvar} desugar.
 */
public final class BoxWriteNode extends GoStmtNode {
    @Child private GoExprNode value;
    private final GoKind kind;
    private final int cellSlot;

    public BoxWriteNode(GoKind kind, int cellSlot, GoExprNode value) {
        this.kind = kind;
        this.cellSlot = cellSlot;
        this.value = value;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object v = Boxing.copyForAssign(kind, Boxing.box(kind, value, frame));
        Boxing.setBoxedCell(frame, cellSlot, kind, v);
    }
}
