package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;
import graalgo.safepoint.Safepoint;

/**
 * The repeating body of a {@code for} statement (WS E, D8).
 * <p>
 * One {@code executeRepeating} call is one iteration: run the per-iteration
 * prologue, check the condition, run the body, run the post statement, then
 * {@link Safepoint#poll()} at the back-edge. {@code continue} (with a
 * matching label, or unlabeled) resumes at the post statement; {@code break}
 * propagates to the enclosing {@link ForStmtNode}, which checks the label.
 *
 * <p>The prologue is null for unboxed loops; for loops with boxed
 * {@code for}-header variables (M6) it refreshes the body's per-iteration
 * cells from the outer cells before the condition, giving each iteration
 * its own variable (gc go1.22+ semantics). It runs <em>before</em> the
 * condition so a failed first condition still performs one refresh
 * (observably equivalent; the refresh is side-effect-free beyond the
 * fresh cell).
 */
public final class ForRepeatingNode extends Node implements RepeatingNode {
    @Child private GoExprNode cond; // nullable: infinite loop
    @Child private GoStmtNode post; // nullable
    @Child private GoStmtNode body;
    @Child private GoStmtNode prologue; // nullable: M6 per-iteration cell refresh
    private String label; // nullable: this loop's label, for labeled continue

    public ForRepeatingNode(GoExprNode cond, GoStmtNode post, GoStmtNode body) {
        this(cond, post, body, null);
    }

    public ForRepeatingNode(GoExprNode cond, GoStmtNode post, GoStmtNode body, GoStmtNode prologue) {
        this.cond = cond;
        this.post = post;
        this.body = body;
        this.prologue = prologue;
    }

    public void setLabel(String label) {
        this.label = label;
    }

    @Override
    public boolean executeRepeating(VirtualFrame frame) {
        if (prologue != null) {
            prologue.executeVoid(frame);
        }
        if (cond != null && !cond.executeBoolean(frame)) {
            return false;
        }
        try {
            body.executeVoid(frame);
        } catch (ContinueSignal s) {
            if (!s.matches(label)) {
                throw s; // targets an outer loop
            }
            // else: fall through to the post statement
        }
        if (post != null) {
            post.executeVoid(frame);
        }
        Safepoint.poll(); // D8 seam at the loop back-edge
        return true;
    }
}
