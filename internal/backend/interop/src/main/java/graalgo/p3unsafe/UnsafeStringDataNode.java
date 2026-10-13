package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * {@code unsafe.StringData(s)} (P3-M8): the {@code "unsafestringdata"}
 * wire node. The empty string yields a nil {@code *byte} (Java null; gc:
 * {@code StringData("") == nil}); otherwise the {@code GoString} is
 * interned (identity) and wrapped in a {@code GoPointer}, so repeated calls
 * on the same string compare equal.
 *
 * <p>P3-M8b: the Go type is {@code *byte} (StringData is generic since Go
 * 1.20), so the kind is POINTER, not UNSAFE_POINTER.
 */
public final class UnsafeStringDataNode extends GoExprNode {
    @Child private GoExprNode strChild;

    private UnsafeStringDataNode(GoExprNode strChild) {
        super(GoKind.POINTER);
        this.strChild = strChild;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object s = strChild.executeObject(frame);
        long handle = UnsafeOps.stringData((GoString) s);
        if (handle == 0) {
            return null; // nil *byte: Java null per the POINTER convention.
        }
        return UnsafeTable.pointerFor(UnsafeTable.current().resolve(handle));
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
