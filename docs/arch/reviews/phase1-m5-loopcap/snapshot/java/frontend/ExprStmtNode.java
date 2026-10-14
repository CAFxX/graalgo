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
        expr.executeLong(frame);
    }
}
