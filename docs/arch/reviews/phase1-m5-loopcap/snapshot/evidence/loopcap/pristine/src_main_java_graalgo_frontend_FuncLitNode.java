package graalgo.frontend;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoBox;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * A function literal value (M5, WS F).
 *
 * <p>Creates a {@link GoFunc}: the compiled body ({@link RootCallTarget})
 * plus the materialized parent frame when the literal captures variables
 * (null otherwise). The body is a {@link GoFuncRootNode}; calls go through
 * {@link FuncValueCallNode}, deferred calls through {@link DeferNode}.
 *
 * <p>Per-iteration capture (M6): when the literal captures boxed (loop)
 * variables, the creation spec lists {@code (depth, slot, snapSlot)}
 * triples in a fixed order. At creation the node walks the
 * {@code $closure} chain (slot 0 of every function-literal frame — the
 * {@code $closure} slot is always allocated first) to the declaring
 * frame, reads the current {@link GoBox} cell, and stores it on the
 * {@code GoFunc}; the body root installs the cell in its own snapshot
 * slot on entry. The spec is resolved against the <em>creating</em> frame
 * at creation time, so each iteration's closure sees that iteration's
 * cell. There is no write-through to an outer cell (gc-oracle verified).
 */
public final class FuncLitNode extends GoExprNode {
    private final RootCallTarget target;
    private final boolean captures;
    /** Snapshot spec: (depth, slot, snapSlot) triples; null when none. */
    private final int[] boxSpec;
    /** Creating frame's $closure slot (0 for funclits; unused when boxSpec is null). */
    private final int closureSlot;

    public FuncLitNode(RootCallTarget target, boolean captures) {
        this(target, captures, null, 0);
    }

    public FuncLitNode(RootCallTarget target, boolean captures, int[] boxSpec, int closureSlot) {
        super(GoKind.FUNC);
        this.target = target;
        this.captures = captures;
        this.boxSpec = boxSpec;
        this.closureSlot = closureSlot;
        if (boxSpec != null && boxSpec.length % 3 != 0) {
            throw new IllegalArgumentException("boxSpec must hold (depth, slot, snapSlot) triples");
        }
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // M5 (WS F): always materialize the parent frame, not just when
        // this literal captures. Nested literals may capture through this
        // function's frame, so the chain must be complete.
        MaterializedFrame cf = frame.materialize();
        if (boxSpec == null) {
            return new GoFunc(target, cf);
        }
        int n = boxSpec.length / 3;
        GoBox[] cells = new GoBox[n];
        for (int i = 0; i < n; i++) {
            int depth = boxSpec[i * 3];
            int slot = boxSpec[i * 3 + 1];
            MaterializedFrame f = cf;
            for (int d = 0; d < depth; d++) {
                // Hop 0 leaves the creating frame (its $closure slot is
                // closureSlot); deeper hops leave function-literal frames
                // whose $closure slot is always 0.
                f = (MaterializedFrame) f.getObject(d == 0 ? closureSlot : 0);
            }
            cells[i] = (GoBox) f.getObject(slot);
            if (cells[i] == null) {
                throw new IllegalStateException("FuncLitNode: null cell at depth " + depth
                        + " slot " + slot + " (per-iteration cell not installed)");
            }
        }
        return new GoFunc(target, cf, cells);
    }
}
