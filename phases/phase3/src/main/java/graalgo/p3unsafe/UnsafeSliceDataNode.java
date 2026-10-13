package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * {@code unsafe.SliceData(s)} (P3-M8): the {@code "unsafeslicedata"} wire
 * node. A nil slice yields a nil {@code *T} (Java null); otherwise the
 * slice's backing array is interned as an {@link UnsafeTable.ArrayRef} view
 * and wrapped in a {@code GoPointer}.
 *
 * <p>P3-M8b: the Go type is {@code *byte} (SliceData is generic since Go
 * 1.20), so the kind is POINTER, not UNSAFE_POINTER. The handle is resolved
 * to its referent immediately; consumers (e.g. {@code unsafe.Slice}) expect
 * a {@code GoPointer}.
 */
public final class UnsafeSliceDataNode extends GoExprNode {
    @Child private GoExprNode sliceChild;

    private UnsafeSliceDataNode(GoExprNode sliceChild) {
        super(GoKind.POINTER);
        this.sliceChild = sliceChild;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object s = sliceChild.executeObject(frame);
        long handle = UnsafeOps.sliceData((GoSlice) s);
        if (handle == 0) {
            return null; // nil *T: Java null per the POINTER convention.
        }
        return UnsafeTable.pointerFor(UnsafeTable.current().resolve(handle));
    }

    /**
     * Builds the node from the {@code "unsafeslicedata"} wire node. Fields:
     * {@code slice}, {@code type} (the {@code *T} result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafeslicedata");
        int typeId = UnsafeWire.requireTypeId(f, "unsafeslicedata");
        GoExprNode n = new UnsafeSliceDataNode(
                UnsafeWire.child(f, "unsafeslicedata", "slice", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
