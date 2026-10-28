package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * Provenance-preserving {@code uintptr → unsafe.Pointer} (fix-redmaster-r39):
 * the {@code "unsafederive"} wire node. The frontend emits this node only
 * when it has proven — from the go/types-annotated AST at the conversion
 * site — that the {@code uintptr} operand is exactly one
 * {@code unsafe.Pointer}-derived term plus pure-integer terms (the
 * {@code indexLocal} shape:
 * {@code unsafe.Pointer(uintptr(l) + uintptr(i)*unsafe.Sizeof(T))}).
 * The base {@code unsafe.Pointer} expression and the integer byte offset
 * travel as separate children, so no handle is ever materialized from a
 * bare integer: the result is {@link UnsafeTable#derive} of the live base
 * handle by the offset — a view of the base's own referent, or a loud
 * trap. Any {@code uintptr} whose provenance the frontend cannot prove
 * keeps the D5 {@code "unsafefromuint"} trap
 * ({@link UnsafeFromUintNode}).
 */
public final class UnsafeDeriveNode extends GoExprNode {
    @Child private GoExprNode baseChild;
    @Child private GoExprNode offsetChild;

    private UnsafeDeriveNode(GoExprNode baseChild, GoExprNode offsetChild) {
        super(GoKind.UNSAFE_POINTER);
        this.baseChild = baseChild;
        this.offsetChild = offsetChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long base = baseChild.executeLong(frame);
        long offset = offsetChild.executeLong(frame);
        return UnsafeTable.current().derive(base, offset);
    }

    /**
     * Builds the node from the {@code "unsafederive"} wire node. Fields:
     * {@code base} (the {@code unsafe.Pointer}-valued operand),
     * {@code offset} (the integer byte-offset operand), {@code type}
     * (result type-table id — the {@code unsafe.Pointer} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafederive");
        int typeId = UnsafeWire.requireTypeId(f, "unsafederive");
        GoExprNode n = new UnsafeDeriveNode(
                UnsafeWire.child(f, "unsafederive", "base", factory, ctx),
                UnsafeWire.child(f, "unsafederive", "offset", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
