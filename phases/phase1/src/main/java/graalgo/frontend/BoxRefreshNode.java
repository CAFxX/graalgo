package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Per-iteration refresh of a {@code for}-header variable's cell (loopcap
 * rework, gc model).
 *
 * <p>Runs at the top of every loop iteration, before the condition:
 * installs a fresh {@link GoBox} in the per-iteration cell slot, seeded
 * with a copy of the <em>carry</em> cell's current value (Go's
 * {@code z := z'}). The copy follows Go's assignment rule
 * ({@link Boxing#copyForAssign}): struct/array values are copied,
 * references alias. There is no link back to the carry: the per-iteration
 * cell is fully detached, so a closure that captured an older iteration's
 * cell can never corrupt the value that seeds future iterations. The
 * carry is updated only by the explicit end-of-iteration
 * {@link BoxWritebackNode}.
 */
public final class BoxRefreshNode extends GoStmtNode {
    private final GoKind kind;
    private final int cellSlot;
    private final int carrySlot;

    public BoxRefreshNode(GoKind kind, int cellSlot, int carrySlot) {
        this.kind = kind;
        this.cellSlot = cellSlot;
        this.carrySlot = carrySlot;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        GoBox carry = (GoBox) frame.getObject(carrySlot);
        if (carry == null) {
            throw new IllegalStateException("BoxRefreshNode: null carry cell in slot " + carrySlot);
        }
        frame.setObject(cellSlot, new GoBox(Boxing.copyForAssign(kind, carry.value)));
    }
}
