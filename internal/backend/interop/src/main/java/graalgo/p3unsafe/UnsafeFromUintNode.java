package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code uintptr → unsafe.Pointer} (P3-M8): the {@code "unsafefromuint"}
 * wire node. The frontend emits it only when it cannot prove the
 * operand's provenance (see {@code unsafe_derive.go}); at runtime the
 * conversion panics with the documented D5 message
 * (review-20261031 R-06: a Go panic, recoverable by guest code and
 * exiting 2 uncaught — never a host internal error, and never a
 * silently wrong pointer). The operand still evaluates (gc evaluates
 * the argument).
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
        UnsafeOps.fromUintPanic(this);
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
