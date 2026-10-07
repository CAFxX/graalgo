package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * {@code unsafe.StringData(s)} (P3-M8): the {@code "unsafestringdata"}
 * wire node. The empty string yields handle {@code 0} (gc:
 * {@code StringData("") == nil}); otherwise the {@code GoString} is
 * interned (identity), so repeated calls on the same string compare equal.
 */
public final class UnsafeStringDataNode extends GoExprNode {
    @Child private GoExprNode strChild;

    private UnsafeStringDataNode(GoExprNode strChild) {
        super(GoKind.UNSAFE_POINTER);
        this.strChild = strChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object s = strChild.executeObject(frame);
        return UnsafeOps.stringData((GoString) s);
    }

    /**
     * Builds the node from the {@code "unsafestringdata"} wire node.
     * Fields: {@code s}, {@code type} (the {@code *byte} result type-table
     * id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafestringdata");
        int typeId = UnsafeWire.requireTypeId(f, "unsafestringdata");
        GoExprNode n = new UnsafeStringDataNode(
                UnsafeWire.child(f, "unsafestringdata", "s", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
