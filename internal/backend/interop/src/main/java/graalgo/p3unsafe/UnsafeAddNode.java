package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code unsafe.Add(ptr, len)} (P3-M8): the {@code "unsafeadd"} wire node.
 * The length is a byte count (gc: {@code Add} is
 * {@code Pointer(uintptr(ptr) + uintptr(len))}). Since fix-redmaster-r39
 * the result is {@link UnsafeTable#derive} of the live base handle by
 * that many bytes — a view of the base's own referent that the typed
 * consumption sites validate — instead of the former unconditional D5
 * trap. Both operands still evaluate first, matching gc's argument
 * evaluation order. The nil-base trap ({@code Add(nil, len != 0)})
 * surfaces as a Go panic (review finding R5), converted from the
 * low-level trap via {@link UnsafeOps#panicFromTrap} exactly as in
 * {@link UnsafeDeriveNode}; other {@code derive} failures still
 * propagate as internal errors.
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
        long ptr = ptrChild.executeLong(frame);
        long len = lenChild.executeLong(frame);
        try {
            return UnsafeTable.current().derive(ptr, len);
        } catch (graalgo.values.UnsafeHandles.UnsafeEscapeException e) {
            // See UnsafeDeriveNode: with a nil base this is exactly the
            // nil-base trap, a guest error surfaced as a Go panic.
            if (ptr == 0) {
                UnsafeOps.panicFromTrap(e, this);
            }
            throw e;
        }
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
