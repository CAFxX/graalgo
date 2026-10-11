package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;

/**
 * A {@code range} statement: optional init statement, then the loop (WS E).
 * <p>
 * The iteration logic lives in the {@link com.oracle.truffle.api.nodes.RepeatingNode}
 * built by {@code buildRange} (slice/array/string/map variants); this node
 * runs the init statement and catches {@link BreakSignal} for this loop,
 * with the same label rule as {@link ForStmtNode}.
 */
public final class RangeStmtNode extends GoStmtNode implements LabelTarget {
    @Child private GoStmtNode init; // nullable
    @Child private GoLoopNode loop;
    private String label; // nullable

    public RangeStmtNode(GoStmtNode init, GoLoopNode loop) {
        this.init = init;
        this.loop = loop;
    }

    /** Called by buildLabeled when this loop carries a label. */
    @Override
    public void setLabel(String label) {
        this.label = label;
        ((LabelTarget) loop.getRepeatingNode()).setLabel(label);
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        if (init != null) {
            init.executeVoid(frame);
        }
        // Reset the repeating node's iteration state: the same node object
        // executes once per range statement evaluation (e.g. an inner range
        // inside an outer loop, or a range in a function called multiple times).
        Object repeating = loop.getRepeatingNode();
        if (repeating instanceof RangeSliceNode n) {
            n.reset();
        } else if (repeating instanceof RangeArrayNode n) {
            n.reset();
        } else if (repeating instanceof RangeStringNode n) {
            n.reset();
        } else if (repeating instanceof RangeMapNode n) {
            n.reset();
        } else if (repeating instanceof RangeIntNode n) {
            n.reset();
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
