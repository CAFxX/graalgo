package graalgo.p3unsafe;

import java.nio.charset.StandardCharsets;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.UnsafeHandles;

/**
 * {@code uintptr → unsafe.Pointer} (P3-M8): the {@code "unsafefromuint"}
 * wire node. Always traps (D5): a handle can never be materialized from
 * an integer. The operand still evaluates (gc evaluates the argument). The
 * trap surfaces as a Go panic carrying the documented message — never a
 * host-side internal-error crash (P1-3, review-r1).
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
        } catch (UnsafeHandles.UnsafeEscapeException e) {
            // P1-3 (review-r1): convert the trap to a Go panic carrying the
            // documented message, never a host-side internal-error crash.
            // The value must be a D5 GoString, not a Java String —
            // PanicRendering renders Java Strings as "(?) 0xADDR" (P3-M4).
            Panic.throwPanic(new GoString(e.getMessage().getBytes(StandardCharsets.UTF_8)),
                    null, null, this);
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
