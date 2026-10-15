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
 * {@code unsafe.Add(ptr, len)} (P3-M8): the {@code "unsafeadd"} wire node.
 * Always traps (D5/GG-0015): handles admit no byte arithmetic. Both
 * operands still evaluate, matching gc's argument evaluation order. The
 * trap surfaces as a Go panic carrying the documented message — never a
 * host-side internal-error crash (P1-3, review-r1).
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
        try {
            UnsafeOps.addTrap();
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
