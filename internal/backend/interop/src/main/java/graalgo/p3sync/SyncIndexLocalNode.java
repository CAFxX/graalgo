package graalgo.p3sync;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * {@code sync.indexLocal} intrinsic (P3-M16, r16): the
 * {@code "syncindexlocal"} wire node.
 *
 * <p>gc implements {@code indexLocal(l, i)} as pointer arithmetic:
 * {@code (*poolLocal)(unsafe.Pointer(uintptr(l) + uintptr(i)*size))}.
 * Under graalgo's D5 forged-pointer rule, materializing a pointer from
 * integer arithmetic traps. This intrinsic implements it directly: {@code l}
 * is always {@code unsafe.Pointer(&arr[0])} for a {@code []poolLocal} slice
 * (see {@code Pool.pinSlow}), so the node resolves the handle to the array
 * base and returns a pointer to the {@code i}-th element.
 *
 * <p>Fields: {@code "l"} (the unsafe.Pointer base, as long handle),
 * {@code "i"} (the index), {@code "type"} (result type-table id —
 * the {@code *poolLocal} type).
 */
public final class SyncIndexLocalNode extends GoExprNode {
    @Child private GoExprNode l;
    @Child private GoExprNode i;

    private SyncIndexLocalNode(GoKind kind, GoExprNode l, GoExprNode i) {
        super(kind);
        this.l = l;
        this.i = i;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long handle = l.executeLong(frame);
        long idx = i.executeLong(frame);
        if (handle == 0) {
            throw new FrontendException("syncindexlocal: nil base pointer");
        }
        Object resolved = UnsafeTable.current().resolve(handle);
        if (!(resolved instanceof UnsafeTable.ArrayRef ref)) {
            throw new FrontendException("syncindexlocal: base is not an array element pointer, got "
                    + (resolved == null ? "null" : resolved.getClass().getSimpleName()));
        }
        // Return pointer to the idx-th element from the base.
        return new GoPointer(ref.array(), ref.offset() + (int) idx);
    }

    /**
     * Builds the node from the {@code "syncindexlocal"} wire node.
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "syncindexlocal");
        int typeId = f.intValue("type", "syncindexlocal.fields");
        GoKind kind = factory.kindOf(ctx, typeId, "syncindexlocal.fields");
        GoExprNode lNode = factory.buildExpr(f.object("l", "syncindexlocal.fields"), ctx);
        GoExprNode iNode = factory.buildExpr(f.object("i", "syncindexlocal.fields"), ctx);
        GoExprNode n = new SyncIndexLocalNode(kind, lNode, iNode);
        n.setTypeId(typeId);
        return n;
    }
}
