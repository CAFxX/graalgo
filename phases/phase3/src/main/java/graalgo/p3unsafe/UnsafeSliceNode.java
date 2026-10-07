package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;

/**
 * {@code unsafe.Slice(ptr, len)} (P3-M8): the {@code "unsafeslice"} wire
 * node. {@code ptr} is {@code *T}-valued (a {@code GoPointer}; the
 * frontend converts the {@code unsafe.Pointer} argument); the referent
 * dispatch lives in {@link UnsafeOps#slice}.
 */
public final class UnsafeSliceNode extends GoExprNode {
    @Child private GoExprNode ptrChild;
    @Child private GoExprNode lenChild;
    private final GoKind elemKind;

    private UnsafeSliceNode(GoExprNode ptrChild, GoExprNode lenChild, GoKind elemKind) {
        super(GoKind.SLICE);
        this.ptrChild = ptrChild;
        this.lenChild = lenChild;
        this.elemKind = elemKind;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object p = ptrChild.executeObject(frame);
        Object referent = p == null ? null : ((GoPointer) p).referent();
        long len = lenChild.executeLong(frame);
        return UnsafeOps.slice(referent, len, elemKind, this);
    }

    /**
     * Builds the node from the {@code "unsafeslice"} wire node. Fields:
     * {@code ptr}, {@code len}, {@code type} (the {@code []T} result
     * type-table id — {@code T}'s carrier comes from its {@code "elem"}
     * detail).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafeslice");
        String path = "unsafeslice.fields";
        int typeId = UnsafeWire.requireTypeId(f, "unsafeslice");
        GoKind elemKind = UnsafeWire.sliceElemKind(ctx, typeId, path);
        GoExprNode n = new UnsafeSliceNode(
                UnsafeWire.child(f, "unsafeslice", "ptr", factory, ctx),
                UnsafeWire.child(f, "unsafeslice", "len", factory, ctx),
                elemKind);
        n.setTypeId(typeId);
        return n;
    }
}
