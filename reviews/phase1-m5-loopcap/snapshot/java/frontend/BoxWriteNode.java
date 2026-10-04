package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * Writes a boxed (per-iteration capture, M6) variable.
 *
 * <p>The frame slot {@code cellSlot} holds a {@link GoBox}; the write
 * stores the canonical boxed value (after Go's value-copy rule) in the
 * cell. When the cell is a {@code for}-header variable's per-iteration
 * body cell, its {@link GoBox#outer} points at the outer cell and the
 * write goes <em>through</em> to the outer cell as well, so init/cond/post
 * observe body mutations exactly as gc does (gc-oracle verified: a
 * closure's write is visible to the post statement, t11). Both cells
 * receive independent copies for struct/array values.
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
