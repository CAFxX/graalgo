package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;
import graalgo.values.GoMap;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code delete} builtin (WS G; verifies GG-0020).
 *
 * <p>{@code delete(m, k)} removes {@code k} from map {@code m}. It is a
 * statement-only builtin (no result): {@link #executeLong} is implemented so
 * {@link ExprStmtNode} can execute it (statement builtins must implement
 * {@code executeLong}).
 */
public final class DeleteNode extends GoExprNode {
    @Child private GoExprNode mapChild;
    @Child private GoExprNode keyChild;

    public DeleteNode(GoExprNode mapChild, GoExprNode keyChild) {
        // No result; INTERFACE is a placeholder kind (only executeLong is
        // ever called, via ExprStmtNode).
        super(GoKind.INTERFACE);
        this.mapChild = mapChild;
        this.keyChild = keyChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        GoMap m = (GoMap) mapChild.executeObject(frame);
        Object k = keyChild.executeObject(frame);
        m.delete(k);
        return 0L;
    }
}
