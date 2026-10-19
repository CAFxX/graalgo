package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code unsafe.Add(ptr, len)} (P3-M8): the {@code "unsafeadd"} wire node.
 * Always traps (D5/GG-0015): handles admit no byte arithmetic. The trap
 * is a Go panic carrying the documented message — recoverable via Go
 * {@code recover()} — not an internal-error crash (B-6 interim,
 * RV22-P1-8). Both operands still evaluate, matching gc's argument
 * evaluation order.
 */
public final class UnsafeAddNode extends GoExprNode {
    @Child private GoExprNode ptrChild;
    @Child private GoExprNode lenChild;

    private UnsafeAddNode(GoExprNode ptrChild, GoExprNode lenChild) {
        super(GoKind.UNSAFE_POINTER);
        this.ptrChild = ptrChild;
        this.lenChild = lenChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        ptrChild.executeLong(frame);
        lenChild.executeLong(frame);
        UnsafeOps.addTrap(this);
        throw new AssertionError("unreachable");
    }

    /**
     * Builds the node from the {@code "unsafeadd"} wire node. Fields:
     * {@code ptr}, {@code len}, {@code type} (the {@code unsafe.Pointer}
     * result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafeadd");
        int typeId = UnsafeWire.requireTypeId(f, "unsafeadd");
        GoExprNode n = new UnsafeAddNode(
                UnsafeWire.child(f, "unsafeadd", "ptr", factory, ctx),
                UnsafeWire.child(f, "unsafeadd", "len", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
