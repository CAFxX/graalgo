package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;

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

    public BlockNode(GoStmtNode[] stmts) {
        this.stmts = stmts;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
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
