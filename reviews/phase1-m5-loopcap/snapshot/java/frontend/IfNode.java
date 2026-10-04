package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;

/** {@code if} / {@code else} (M4, WS E). The init statement may be null. */
public final class IfNode extends GoStmtNode {
    @Child private GoStmtNode init;
    @Child private GoExprNode cond;
    @Child private GoStmtNode then;
    @Child private GoStmtNode els;

    public IfNode(GoStmtNode init, GoExprNode cond, GoStmtNode then, GoStmtNode els) {
        this.init = init;
        this.cond = cond;
        this.then = then;
        this.els = els;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        if (init != null) {
            init.executeVoid(frame);
        }
        if (cond.executeBoolean(frame)) {
            then.executeVoid(frame);
        } else if (els != null) {
            els.executeVoid(frame);
        }
    }
}
