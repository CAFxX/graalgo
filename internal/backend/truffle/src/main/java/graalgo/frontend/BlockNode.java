package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import com.oracle.truffle.api.nodes.Node;
import graalgo.safepoint.Safepoint;

/**
 * Sequential statement list (M4, WS E: goto dispatch).
 *
 * <p>A {@link GotoSignal} thrown by any statement is caught here: the
 * block scans its direct children for the {@link LabeledStmtNode} carrying
 * the label and resumes execution there (program-counter dispatch).
 * Labels are function-scoped, so the innermost block that directly
 * contains the labeled statement is always an ancestor of the throw site —
 * inner blocks that don't contain it rethrow. go/types guarantees the
 * target is reachable (no jumping into blocks or over declarations), so an
 * uncaught signal is an internal error.
 */
public final class BlockNode extends GoStmtNode {
    @Children private final GoStmtNode[] stmts;

    /**
     * True when some statement of this block is a goto target. Blocks
     * without labels (the overwhelmingly common case) run as a straight,
     * PE-unrolled sequence: a goto from inside them can only target an
     * enclosing block, so the signal propagates exactly as before.
     */
    private final boolean hasLabels;

    public BlockNode(GoStmtNode[] stmts) {
        this.stmts = stmts;
        boolean labels = false;
        for (GoStmtNode s : stmts) {
            labels |= s instanceof LabeledStmtNode;
        }
        this.hasLabels = labels;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        if (!hasLabels) {
            executeStraight(frame);
            return;
        }
        executeWithGotos(frame);
    }

    @ExplodeLoop
    private void executeStraight(VirtualFrame frame) {
        for (GoStmtNode s : stmts) {
            s.executeVoid(frame);
        }
    }

    private void executeWithGotos(VirtualFrame frame) {
        int pc = 0;
        while (pc < stmts.length) {
            try {
                for (; pc < stmts.length; pc++) {
                    stmts[pc].executeVoid(frame);
                }
            } catch (GotoSignal g) {
                int target = findLabel(g.label());
                if (target < 0) {
                    throw g;
                }
                if (target <= pc) {
                    // A backward goto is a loop back-edge: poll like every
                    // other loop (quantum preemption, exit freeze), or a
                    // goto loop starves the other goroutines under a cap.
                    Safepoint.poll(this);
                }
                pc = target;
            }
        }
    }

    private int findLabel(String label) {
        for (int i = 0; i < stmts.length; i++) {
            if (stmts[i] instanceof LabeledStmtNode l && l.label().equals(label)) {
                return i;
            }
        }
        return -1;
    }
}
