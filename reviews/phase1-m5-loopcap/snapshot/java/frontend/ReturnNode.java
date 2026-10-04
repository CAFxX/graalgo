package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * {@code return [x]} (M5, WS F).
 *
 * <p>Assigns the value to the function result slot when the enclosing
 * function has one (function literals with results; the slot holds the
 * named result variable, or an allocated slot for unnamed results), then
 * throws {@link GoReturn}. The owning root node runs deferred calls
 * <em>before</em> reading the result slot, so {@code defer} sees and may
 * modify named returns (the classic semantic trap, gc-verified).
 */
public final class ReturnNode extends GoStmtNode {
    @Child private GoExprNode value;
    private final int resultSlot; // -1 when the enclosing function has no result slot
    private final GoKind resultKind;

    public ReturnNode(GoExprNode value) {
        this(value, -1, null);
    }

    public ReturnNode(GoExprNode value, int resultSlot, GoKind resultKind) {
        this.value = value;
        this.resultSlot = resultSlot;
        this.resultKind = resultKind;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object v = value == null ? null : Boxing.box(value.kind(), value, frame);
        if (resultSlot >= 0 && v != null) {
            Boxing.setSlot(frame, resultSlot, resultKind, v);
        }
        throw new GoReturn(v);
    }
}
