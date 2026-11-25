package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;
import graalgo.values.GoMap;

/**
 * The {@code ok} projection of a comma-ok map index ({@code v, ok := m[k]},
 * M4 WS E). Reports key presence; the value half is a sibling
 * {@link IndexNode} over the same (already evaluated) container/key
 * temporaries.
 */
public final class IndexOkNode extends GoExprNode {
    @Child private GoExprNode container;
    @Child private GoExprNode key;

    public IndexOkNode(GoExprNode container, GoExprNode key) {
        super(GoKind.BOOL);
        this.container = container;
        this.key = key;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        GoMap m = (GoMap) container.executeObject(frame);
        if (m == null) {
            return false;
        }
        return m.containsKey(MapKeys.box(key.kind(), key, frame));
    }
}
