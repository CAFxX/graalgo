package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code uintptr → unsafe.Pointer} (P3-M8): the {@code "unsafefromuint"}
 * wire node. Always traps (D5): a handle can never be materialized from
 * an integer. The operand still evaluates (gc evaluates the argument).
 * The trap surfaces as a Go panic (review finding R5): this node converts
 * the low-level {@link graalgo.values.UnsafeHandles.UnsafeEscapeException}
 * to a panic via {@link UnsafeOps#panicFromTrap} at this known throw site,
 * so the guest program observes a recoverable panic — never a scheduler
 * {@code "fatal internal error"} crash.
 */
public final class UnsafeFromUintNode extends GoExprNode {
    @Child private GoExprNode child;

    private UnsafeFromUintNode(GoExprNode child) {
        super(GoKind.UNSAFE_POINTER);
        this.child = child;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        child.executeLong(frame);
        try {
            UnsafeOps.fromUintTrap();
        } catch (graalgo.values.UnsafeHandles.UnsafeEscapeException e) {
            UnsafeOps.panicFromTrap(e, this);
        }
        throw new AssertionError("unreachable");
    }

    /**
     * Builds the node from the {@code "unsafefromuint"} wire node. Fields:
     * {@code x} (the uintptr operand), {@code type} (result type-table id —
     * the {@code unsafe.Pointer} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafefromuint");
        int typeId = UnsafeWire.requireTypeId(f, "unsafefromuint");
        GoExprNode n = new UnsafeFromUintNode(
                UnsafeWire.child(f, "unsafefromuint", "x", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
