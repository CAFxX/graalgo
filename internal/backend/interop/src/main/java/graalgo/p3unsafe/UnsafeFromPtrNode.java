package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * {@code *T → unsafe.Pointer} (P3-M8): the {@code "unsafefromptr"} wire
 * node. Interns the {@code GoPointer}'s <em>storage identity</em> in
 * {@link UnsafeTable} — the cell for cell pointers ({@code new(T)}
 * scalars, {@code &x}), an {@link UnsafeTable.ArrayRef} for
 * {@code &arr[i]} element pointers, the referent object otherwise —
 * never the dereferenced value. A nil pointer (Java {@code null} child or
 * a nil {@code GoPointer}) maps to handle {@code 0}.
 *
 * <p>P3-M10 (pointer-identity root fix): interning the dereferenced value
 * ({@code gp.referent()}) conflated distinct allocations whose cells hold
 * the same (JVM-shared, cached) zero box — e.g. every {@code new(int)}
 * cell holds the identical {@code Long(0)} — so {@code unsafe.Pointer}
 * of two distinct allocations minted one handle ({@code p1 == q1} was
 * true, gc: false), and a pointer's handle changed across {@code *p = v}
 * writes. Storage-identity interning matches
 * {@link GoPointer#sameStorageAs} and gc in all three cases.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class UnsafeFromPtrNode extends GoExprNode {
    @Child private GoExprNode child;

    private UnsafeFromPtrNode(GoExprNode child) {
        super(GoKind.UNSAFE_POINTER);
        this.child = child;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object p = child.executeObject(frame);
        if (p == null) {
            return 0;
        }
        if (!(p instanceof GoPointer gp)) {
            throw new FrontendException("unsafefromptr: expected a *T value, got "
                    + p.getClass().getSimpleName());
        }
        // P3-M10: intern storage identity, not the dereferenced value.
        // gp.referent() unwraps cell pointers to the cell's *content* —
        // the JVM-shared cached zero box for fresh new(T) — which made
        // distinct allocations share one handle. The rule lives in
        // UnsafeTable.handleForPointer (shared with the interface-view
        // builder); this node keeps only the operand validation.
        return UnsafeTable.handleForPointer(gp);
    }

    /**
     * Builds the node from the {@code "unsafefromptr"} wire node. Fields:
     * {@code x} (the {@code *T}-valued operand), {@code type} (result
     * type-table id — the {@code unsafe.Pointer} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafefromptr");
        int typeId = UnsafeWire.requireTypeId(f, "unsafefromptr");
        GoExprNode n = new UnsafeFromPtrNode(
                UnsafeWire.child(f, "unsafefromptr", "x", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
