package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;

/**
 * A labeled statement {@code L: stmt} (M4, WS E).
 *
 * <p>Two roles: (1) the goto target — the enclosing {@link BlockNode} finds
 * this node by label and resumes execution here; (2) the target of a
 * labeled {@code break} on a non-loop, non-switch statement
 * ({@code L: if ... { break L }}). Loops and switches labeled this way
 * consume their own label via {@link LabelTarget}; this wrapper only
 * catches a {@link BreakSignal} addressed to it.
 */
public final class LabeledStmtNode extends GoStmtNode {
    private final String label;
    @Child private GoStmtNode stmt;

    public LabeledStmtNode(String label, GoStmtNode stmt) {
        this.label = label;
        this.stmt = stmt;
    }

    public String label() {
        return label;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        try {
            stmt.executeVoid(frame);
        } catch (BreakSignal b) {
            if (b.label() == null || !b.label().equals(label)) {
                throw b;
            }
            // break L on a labeled non-loop statement: exit it.
        }
    }
}
