package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * {@code *T → unsafe.Pointer} (P3-M8): the {@code "unsafefromptr"} wire
 * node. Interns the {@code GoPointer}'s referent in {@link UnsafeTable}
 * (identity interning → {@code ==} parity with gc); a nil pointer (Java
 * {@code null} child) maps to handle {@code 0}.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class UnsafeFromPtrNode extends GoExprNode {
    @Child private GoExprNode child;

    private UnsafeFromPtrNode(GoExprNode child) {
        super(GoKind.UNSAFE_POINTER);
        this.child = child;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object p = child.executeObject(frame);
        if (p == null) {
            return 0;
        }
        if (!(p instanceof GoPointer gp)) {
            throw new FrontendException("unsafefromptr: expected a *T value, got "
                    + p.getClass().getSimpleName());
        }
        return UnsafeTable.current().intern(gp.referent());
    }

    /**
     * Builds the node from the {@code "unsafefromptr"} wire node. Fields:
     * {@code x} (the {@code *T}-valued operand), {@code type} (result
     * type-table id — the {@code unsafe.Pointer} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafefromptr");
        int typeId = UnsafeWire.requireTypeId(f, "unsafefromptr");
        GoExprNode n = new UnsafeFromPtrNode(
                UnsafeWire.child(f, "unsafefromptr", "x", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
