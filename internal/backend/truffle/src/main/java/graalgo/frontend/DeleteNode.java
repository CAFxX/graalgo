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
        // No result; INT64 is a placeholder kind (only executeLong is
        // ever called, via ExprStmtNode, which dispatches on slotKind —
        // INTERFACE would route to executeObject and throw).
        super(GoKind.INT64);
        this.mapChild = mapChild;
        this.keyChild = keyChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        GoMap m = (GoMap) mapChild.executeObject(frame);
        // M7: canonicalize the key via MapKeys.box (int keys box as Long,
        // etc.), matching the index-write path — otherwise delete(m, 1)
        // won't find a key stored as Long by m[1] = ....
        Object k = MapKeys.box(keyChild.kind(), keyChild, frame);
        // P3-M4 (WS C): delete on a nil map is a no-op (gc parity).
        if (m == null) {
            return 0L;
        }
        m.delete(k);
        return 0L;
    }
}
