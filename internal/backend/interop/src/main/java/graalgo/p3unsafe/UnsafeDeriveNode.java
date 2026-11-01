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
 * ({@link UnsafeFromUintNode}). The one trap this node itself can hit —
 * derivation from a nil base (base handle {@code 0}) with a nonzero
 * offset — surfaces as a Go panic (review finding R5): the catch in
 * {@link #executeLong} converts it via {@link UnsafeOps#panicFromTrap}
 * only when the base is nil, so any other {@code UnsafeEscapeException}
 * from {@code derive} (e.g. an unknown handle, an internal invariant
 * violation) still propagates as an internal error instead of
 * masquerading as a guest panic.
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
        try {
            return UnsafeTable.current().derive(base, offset);
        } catch (graalgo.values.UnsafeHandles.UnsafeEscapeException e) {
            // Only the nil-base trap is a guest error: derive returns the
            // base unchanged for offset 0, so a throw with base 0 is
            // exactly the nil-base-with-nonzero-offset trap. Anything
            // else (unknown handle) is an internal invariant violation.
            if (base == 0) {
                UnsafeOps.panicFromTrap(e, this);
            }
            throw e;
        }
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
