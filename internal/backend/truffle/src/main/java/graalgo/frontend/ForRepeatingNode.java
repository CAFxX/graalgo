package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;
import graalgo.safepoint.Safepoint;

/**
 * The repeating body of a {@code for} statement (WS E, D8).
 * <p>
 * Two execution orders, selected by {@code firstIterSlot}:
 * <ul>
 *   <li>Unboxed loops ({@code firstIterSlot < 0}): run the per-iteration
 *       prologue (null), check the condition, run the body, run the post
 *       statement, then {@link Safepoint#poll()} at the back-edge.
 *       {@code continue} (with a matching label, or unlabeled) resumes at
 *       the post statement; {@code break} propagates to the enclosing
 *       {@link ForStmtNode}, which checks the label.</li>
 *   <li>Loops with boxed {@code for}-header variables
 *       ({@code firstIterSlot >= 0}, a boolean frame slot reset to true by
 *       {@link ForStmtNode} before the loop starts): gc's
 *       {@code loopvar} desugar order. Run the prologue (fresh
 *       per-iteration cell {@code z := z'} from the carry), then — unless
 *       this is the first iteration — the post statement (on the fresh
 *       per-iteration cell), then the condition, then the body, then the
 *       write-back ({@code z' = z}), then {@link Safepoint#poll()} at the
 *       back-edge. {@code continue} is rewritten to the write-back
 *       (gc's {@code goto next}); {@code break}, panics, {@code return},
 *       and {@code goto}-out propagate before it.</li>
 * </ul>
 * <p>
 * The post-before-cond placement is what makes per-iteration capture
 * correct: the post mutates the <em>current</em> iteration's fresh cell,
 * so a closure that captured an <em>earlier</em> iteration's cell never
 * observes the post's increment (gc-oracle verified: cf13 body closures
 * sum 0+1+2+3+4=10, while closures created in the post itself observe the
 * incremented values 1,2,3 — both from the same gc binary).
 *
 * <p>The prologue runs <em>before</em> the condition so a failed first
 * condition still performs one refresh (observably equivalent; the refresh
 * is side-effect-free beyond the fresh cell).
 */
public final class ForRepeatingNode extends Node implements RepeatingNode {
    @Child private GoExprNode cond; // nullable: infinite loop
    @Child private GoStmtNode post; // nullable
    @Child private GoStmtNode body;
    @Child private GoStmtNode prologue; // nullable: per-iteration cell refresh
    @Child private GoStmtNode writeback; // nullable: carry write-back
    /**
     * Boolean frame slot holding gc's {@code tmp_first}: true before the
     * first iteration (set by {@link ForStmtNode}), so the post statement
     * is skipped once. Negative for unboxed loops (legacy post-at-end
     * order).
     */
    private final int firstIterSlot;
    private String label; // nullable: this loop's label, for labeled continue

    public ForRepeatingNode(GoExprNode cond, GoStmtNode post, GoStmtNode body) {
        this(cond, post, body, null, null, -1);
    }

    public ForRepeatingNode(GoExprNode cond, GoStmtNode post, GoStmtNode body, GoStmtNode prologue) {
        this(cond, post, body, prologue, null, -1);
    }

    public ForRepeatingNode(GoExprNode cond, GoStmtNode post, GoStmtNode body,
                            GoStmtNode prologue, GoStmtNode writeback) {
        this(cond, post, body, prologue, writeback, -1);
    }

    public ForRepeatingNode(GoExprNode cond, GoStmtNode post, GoStmtNode body,
                            GoStmtNode prologue, GoStmtNode writeback, int firstIterSlot) {
        this.cond = cond;
        this.post = post;
        this.body = body;
        this.prologue = prologue;
        this.writeback = writeback;
        this.firstIterSlot = firstIterSlot;
    }

    public void setLabel(String label) {
        this.label = label;
    }

    @Override
    public boolean executeRepeating(VirtualFrame frame) {
        if (prologue != null) {
            prologue.executeVoid(frame);
        }
        if (firstIterSlot >= 0) {
            // gc loopvar model: the post statement runs at the top of the
            // iteration, on the fresh per-iteration cell, before the
            // condition — and is skipped on the first iteration.
            if (frame.getBoolean(firstIterSlot)) {
                frame.setBoolean(firstIterSlot, false);
            } else if (post != null) {
                post.executeVoid(frame);
            }
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
            // gc loopvar desugar rewrites continue to "goto next": the
            // write-back runs, then the next iteration starts with a
            // refresh and (non-first) the post statement.
        }
        if (firstIterSlot < 0 && post != null) {
            post.executeVoid(frame); // unboxed loops: post at the end
        }
        if (writeback != null) {
            writeback.executeVoid(frame);
        }
        Safepoint.poll(); // D8 seam at the loop back-edge
        return true;
    }
}
