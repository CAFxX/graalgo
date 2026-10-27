package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code unsafe.Pointer(uintptr(p) + x)} for a general byte offset {@code x}
 * (fix-redmaster-r27): the {@code "unsafeptraddbytes"} wire node.
 * Byte-offset pointer arithmetic on a proven-valid handle.
 *
 * <p>The frontend only emits this node when the conversion argument is
 * structurally pointer arithmetic ({@code uintptr(p) +/- x} with {@code p:
 * unsafe.Pointer}); an opaque integer still traps via
 * {@code "unsafefromuint"} (D5/GG-0015 is unchanged for genuinely
 * integer-derived pointers). The backend resolves the byte offset against
 * the base referent's layout — a struct field (recursing into nested
 * struct/array/slice fields for interior offsets), an array/slice element
 * (with alignment and bounds checks), or a cell's contents — and traps
 * for bases it cannot address (nil, scalars, misaligned or out-of-range
 * offsets, one-past-end struct offsets). This is the mechanism behind
 * reflect's {@code add()} (field addresses, {@code arrayAt} with runtime
 * sizes), not a package- or symbol-keyed special case: any Go code using
 * the byte-offset idiom lowers through this node.
 *
 * <p>Limitations, mirroring the existing field-cell model: a field pointer
 * is a {@link graalgo.frontend.GoFieldCell} over the struct value observed
 * at arithmetic time, so a whole-struct reassignment of the owner variable
 * after the arithmetic does not retarget the field cell (the same snapshot
 * semantics as {@code &s.f} evaluation). Zero-sized element types trap for
 * nonzero offsets (their address is not an array element). Offset 0 is the
 * identity except for struct bases (including cells holding structs),
 * where it yields a field-0 cell: the struct pointer itself is not
 * dereferenceable as a field type.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class UnsafePtrAddBytesNode extends GoExprNode {
    @Child private GoExprNode ptrChild;
    @Child private GoExprNode offsetChild;

    private UnsafePtrAddBytesNode(GoExprNode ptrChild, GoExprNode offsetChild) {
        super(GoKind.UNSAFE_POINTER);
        this.ptrChild = ptrChild;
        this.offsetChild = offsetChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long ptrHandle = ptrChild.executeLong(frame);
        long byteOffset = offsetChild.executeLong(frame);
        return UnsafeOps.ptrAddBytes(ptrHandle, byteOffset);
    }

    /**
     * Builds the node from the {@code "unsafeptraddbytes"} wire node.
     * Fields: {@code ptr} (the base unsafe.Pointer handle), {@code offset}
     * (the signed byte offset), {@code type} (the {@code unsafe.Pointer}
     * result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafeptraddbytes");
        int typeId = UnsafeWire.requireTypeId(f, "unsafeptraddbytes");
        GoExprNode n = new UnsafePtrAddBytesNode(
                UnsafeWire.child(f, "unsafeptraddbytes", "ptr", factory, ctx),
                UnsafeWire.child(f, "unsafeptraddbytes", "offset", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
