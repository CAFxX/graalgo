package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code unsafe.Pointer(uintptr(p) + k*unsafe.Sizeof(T))} (fix-redmaster-conformance):
 * the {@code "unsafeptradd"} wire node. Element-indexed pointer arithmetic.
 *
 * <p>The frontend only emits this node when the offset is structurally a
 * whole number of {@code T}-sized elements ({@code k*unsafe.Sizeof(T)}); the
 * {@code "index"} field is that element count {@code k}, not a byte offset.
 * The backend implements it as {@link UnsafeTable.ArrayRef} element
 * indexing: the base handle must resolve to an {@code ArrayRef} (the
 * storage-identity interning in {@link UnsafeFromPtrNode} produces these
 * for {@code &arr[i]} element pointers and {@code SliceData} views), and
 * the result is a new {@code ArrayRef} {@code k} elements forward.
 *
 * <p>This is not byte arithmetic on an opaque handle (D5/GG-0015): the base
 * is a proven-valid handle (not an arbitrary integer), and the offset is a
 * proven-whole element count (not an arbitrary byte offset). A base that is
 * not an array-element reference, or an out-of-range index, still traps —
 * the D5 discipline (no handle materialized from an integer) is unchanged.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class UnsafePtrAddNode extends GoExprNode {
    @Child private GoExprNode ptrChild;
    @Child private GoExprNode indexChild;

    private UnsafePtrAddNode(GoExprNode ptrChild, GoExprNode indexChild) {
        super(GoKind.UNSAFE_POINTER);
        this.ptrChild = ptrChild;
        this.indexChild = indexChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long ptrHandle = ptrChild.executeLong(frame);
        long index = indexChild.executeLong(frame);
        return UnsafeOps.ptrAdd(ptrHandle, index);
    }

    /**
     * Builds the node from the {@code "unsafeptradd"} wire node. Fields:
     * {@code ptr} (the base unsafe.Pointer handle), {@code index} (the
     * element-count offset), {@code type} (the {@code unsafe.Pointer}
     * result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafeptradd");
        int typeId = UnsafeWire.requireTypeId(f, "unsafeptradd");
        GoExprNode n = new UnsafePtrAddNode(
                UnsafeWire.child(f, "unsafeptradd", "ptr", factory, ctx),
                UnsafeWire.child(f, "unsafeptradd", "index", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
