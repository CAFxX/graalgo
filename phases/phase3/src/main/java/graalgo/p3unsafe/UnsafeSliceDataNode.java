package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * {@code unsafe.SliceData(s)} (P3-M8): the {@code "unsafeslicedata"} wire
 * node. A nil slice yields handle {@code 0}; otherwise the slice's
 * backing array is interned as an {@link UnsafeTable.ArrayRef} view.
 */
public final class UnsafeSliceDataNode extends GoExprNode {
    @Child private GoExprNode sliceChild;

    private UnsafeSliceDataNode(GoExprNode sliceChild) {
        super(GoKind.UNSAFE_POINTER);
        this.sliceChild = sliceChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object s = sliceChild.executeObject(frame);
        return UnsafeOps.sliceData((GoSlice) s);
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
