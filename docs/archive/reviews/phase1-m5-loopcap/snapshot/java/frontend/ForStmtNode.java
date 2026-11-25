package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;

/**
 * A {@code for} statement: optional init statement, then the loop (WS E).
 * <p>
 * Catches {@link BreakSignal} for this loop: an unlabeled break, or
 * {@code break L} where L is this loop's label (set by {@code buildLabeled}),
 * ends the loop; any other labeled break propagates outward.
 */
public final class ForStmtNode extends GoStmtNode implements LabelTarget {
    @Child private GoStmtNode init; // nullable
    @Child private GoLoopNode loop;
    private String label; // nullable

    public ForStmtNode(GoStmtNode init, GoLoopNode loop) {
        this.init = init;
        this.loop = loop;
    }

    /** Called by buildLabeled when this loop carries a label. */
    @Override
    public void setLabel(String label) {
        this.label = label;
        ((ForRepeatingNode) loop.getRepeatingNode()).setLabel(label);
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        if (init != null) {
            init.executeVoid(frame);
        }
        try {
            loop.execute(frame);
        } catch (BreakSignal s) {
            if (!s.matches(label)) {
                throw s; // targets an outer loop/switch
            }
        }
    }
}
