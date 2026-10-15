package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;

/**
 * An atomic.Pointer/Value zero value as a fresh cell per evaluation (NEW-P0-3).
 *
 * <p>The AtomicReference value model requires the cell itself, not a GoStruct.
 * A var declaration ({@code var p atomic.Pointer[int]}) must evaluate to a
 * fresh AtomicReference each time it executes (e.g. inside a loop); sharing
 * a single cell across evaluations would alias the primitive.
 *
 * <p>Only used for atomic.Pointer/Value. Other sync primitives (Mutex, Pool,
 * ...) keep the GoStruct zero because their real methods operate on struct
 * fields.
 */
public final class SyncPrimitiveZeroNode extends GoExprNode {
    private final String prim;

    public SyncPrimitiveZeroNode(String prim) {
        // The cell is opaque; the kind is never directly observed.
        super(GoKind.INTERFACE);
        this.prim = prim;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // Fresh per evaluation: delegates to the same factory the
        // field-zero and make() paths use (NodeFactory.newSyncPrimitive),
        // so the cell type always matches the value model.
        return NodeFactory.newSyncPrimitive(prim);
    }
}
