package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Per-iteration refresh of a {@code for}-header variable's cell (M6).
 *
 * <p>Runs at the top of every loop iteration, before the condition:
 * installs a fresh {@link GoBox} in the body cell slot, seeded with an
 * independent copy of the outer cell's current value. The body (and the
 * closures it creates) therefore observes a per-iteration variable, while
 * init/cond/post keep operating on the outer cell — exactly gc's
 * go1.22+ loop-variable semantics (gc-oracle verified).
 */
public final class BoxRefreshNode extends GoStmtNode {
    private final GoKind kind;
    private final int cellSlot;
    private final int outerSlot;

    public BoxRefreshNode(GoKind kind, int cellSlot, int outerSlot) {
        this.kind = kind;
        this.cellSlot = cellSlot;
        this.outerSlot = outerSlot;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        GoBox outer = (GoBox) frame.getObject(outerSlot);
        if (outer == null) {
            throw new IllegalStateException("BoxRefreshNode: null outer cell in slot " + outerSlot);
        }
        // The fresh body cell points at the outer cell so body writes
        // (BoxWriteNode) go through to it.
        frame.setObject(cellSlot, new GoBox(Boxing.copyForAssign(kind, outer.value), outer));
    }
}
