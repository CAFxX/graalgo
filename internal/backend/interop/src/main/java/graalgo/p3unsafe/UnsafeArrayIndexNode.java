package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * Array indexing via unsafe pointer arithmetic (fix-redmaster-r12): the
 * {@code "unsafearrayindex"} wire node.
 *
 * <p>Lowers the {@code uintptr(p) + uintptr(i)*unsafe.Sizeof(T)} idiom
 * (e.g. {@code sync.Pool}'s {@code indexLocal}): handle-based element
 * pointer arithmetic without integer-address materialization. The base
 * must be an {@code unsafe.Pointer} handle to an array element (an
 * {@link UnsafeTable.ArrayRef}); the result is the handle for the element
 * {@code i} positions forward.
 *
 * <p>This is structural, not name-keyed: any package's indexLocal-like
 * helper matches the frontend's AST-shape peephole. The D5 trap still
 * applies to all other {@code uintptr -> unsafe.Pointer} shapes.
 *
 * <p>Fields: {@code "base"} (the {@code unsafe.Pointer} base expression),
 * {@code "index"} (the integer index expression), {@code "type"} (result
 * type-table id — the {@code unsafe.Pointer} type).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class UnsafeArrayIndexNode extends GoExprNode {
    @Child private GoExprNode base;
    @Child private GoExprNode index;

    private UnsafeArrayIndexNode(GoExprNode base, GoExprNode index) {
        super(GoKind.UNSAFE_POINTER);
        this.base = base;
        this.index = index;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long h = base.executeLong(frame);
        long idx = index.executeLong(frame);
        Object referent = UnsafeTable.current().resolve(h);
        if (!(referent instanceof UnsafeTable.ArrayRef ar)) {
            throw new FrontendException(
                    "unsafearrayindex: base is not an array element pointer handle");
        }
        long newOffset = (long) ar.offset() + idx;
        if (newOffset < 0 || newOffset > Integer.MAX_VALUE) {
            throw new FrontendException(
                    "unsafearrayindex: index out of range: " + idx);
        }
        return UnsafeTable.current().internEqual(
                new UnsafeTable.ArrayRef(ar.array(), (int) newOffset));
    }

    /**
     * Builds the node from the {@code "unsafearrayindex"} wire node.
     * Fields: {@code base}, {@code index}, {@code type}.
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
            NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafearrayindex");
        int typeId = UnsafeWire.requireTypeId(f, "unsafearrayindex");
        GoExprNode n = new UnsafeArrayIndexNode(
                UnsafeWire.child(f, "unsafearrayindex", "base", factory, ctx),
                UnsafeWire.child(f, "unsafearrayindex", "index", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
