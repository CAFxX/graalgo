package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;

/** Expression evaluated for effect; result discarded. */
public final class ExprStmtNode extends GoStmtNode {
    @Child private GoExprNode expr;

    public ExprStmtNode(GoExprNode expr) {
        this.expr = expr;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        // Discard the result via the kind-appropriate read: executeLong on
        // a bool/struct/interface result throws ClassCastException
        // (M6, WS H — issue48276a: IsZero[int](0) as a statement).
        switch (expr.kind().slotKind()) {
            case Int -> expr.executeInt(frame);
            case Long -> expr.executeLong(frame);
            case Float -> expr.executeFloat(frame);
            case Double -> expr.executeDouble(frame);
            case Boolean -> expr.executeBoolean(frame);
            case Object -> expr.executeObject(frame);
        }
    }
}
