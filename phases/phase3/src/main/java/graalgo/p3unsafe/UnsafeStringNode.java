package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * {@code unsafe.String(ptr, len)} (P3-M8): the {@code "unsafestring"} wire
 * node. {@code ptr} is {@code *byte}-valued (a {@code GoPointer}); the
 * referent dispatch and the DIV-US-1 copy live in
 * {@link UnsafeOps#stringOf}.
 */
public final class UnsafeStringNode extends GoExprNode {
    @Child private GoExprNode ptrChild;
    @Child private GoExprNode lenChild;

    private UnsafeStringNode(GoExprNode ptrChild, GoExprNode lenChild) {
        super(GoKind.STRING);
        this.ptrChild = ptrChild;
        this.lenChild = lenChild;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object p = ptrChild.executeObject(frame);
        Object referent = p == null ? null : ((GoPointer) p).referent();
        long len = lenChild.executeLong(frame);
        return UnsafeOps.stringOf(referent, len, this);
    }

    /**
     * Builds the node from the {@code "unsafestring"} wire node. Fields:
     * {@code ptr}, {@code len}, {@code type} (the {@code string} result
     * type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafestring");
        int typeId = UnsafeWire.requireTypeId(f, "unsafestring");
        GoExprNode n = new UnsafeStringNode(
                UnsafeWire.child(f, "unsafestring", "ptr", factory, ctx),
                UnsafeWire.child(f, "unsafestring", "len", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
