package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * End-of-iteration write-back for a {@code for}-header variable (loopcap
 * rework, gc model).
 *
 * <p>gc's {@code loopvar} desugar ends each iteration with {@code z' = z}
 * (write-back of the per-iteration variable into the carry), on the normal
 * path and on {@code continue} paths (continue is rewritten to
 * {@code goto next}, the write-back label), but not on
 * {@code break}/panic/{@code return}/{@code goto}-out paths. This node
 * runs at the end of the iteration, after the body; the post statement
 * runs at the <em>top</em> of the next iteration (on the fresh
 * per-iteration cell, before the condition — skipped on the first
 * iteration), so the placement matches exactly: normal and continue
 * iterations write back; break/panic/return/goto-out propagate before it.
 *
 * <p>The write-back mutates the carry cell in place
 * ({@code carry.value = ...}, never replacing the box object), so a
 * closure created in the init statement — which captures the carry cell,
 * matching gc's {@code init : s/z/z'} substitution — observes every
 * write-back. The copy follows Go's assignment rule
 * ({@link Boxing#copyForAssign}): struct/array values are copied,
 * references alias.
 */
public final class BoxWritebackNode extends GoStmtNode {
    private final GoKind kind;
    private final int iterSlot;
    private final int carrySlot;

    public BoxWritebackNode(GoKind kind, int iterSlot, int carrySlot) {
        this.kind = kind;
        this.iterSlot = iterSlot;
        this.carrySlot = carrySlot;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        GoBox iter = (GoBox) frame.getObject(iterSlot);
        GoBox carry = (GoBox) frame.getObject(carrySlot);
        if (iter == null) {
            throw new IllegalStateException("BoxWritebackNode: null per-iteration cell in slot "
                    + iterSlot);
        }
        if (carry == null) {
            throw new IllegalStateException("BoxWritebackNode: null carry cell in slot "
                    + carrySlot);
        }
        carry.value = Boxing.copyForAssign(kind, iter.value);
    }
}
