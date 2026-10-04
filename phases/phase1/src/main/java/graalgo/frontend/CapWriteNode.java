package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * Writes a variable captured by a function literal in an ancestor frame
 * (M5, WS F). The companion of {@link CapReadNode}: {@code depth} and
 * {@code slot} locate the variable exactly as for reads; the write uses
 * the ancestor slot's kind.
 */
public final class CapWriteNode extends GoStmtNode {
    @Child private GoExprNode value;
    private final GoKind kind;
    private final int depth;
    private final int slot;
    private final int closureSlot;

    public CapWriteNode(GoKind kind, int depth, int slot, int closureSlot, GoExprNode value) {
        this.value = value;
        this.kind = kind;
        this.depth = depth;
        this.slot = slot;
        this.closureSlot = closureSlot;
    }

    private MaterializedFrame targetFrame(VirtualFrame frame) {
        MaterializedFrame f = (MaterializedFrame) frame.getObject(closureSlot);
        for (int i = 0; i < depth; i++) {
            f = (MaterializedFrame) f.getObject(closureSlot);
        }
        return f;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        MaterializedFrame f = targetFrame(frame);
        Object boxed = Boxing.box(kind, value, frame);
        switch (kind.slotKind()) {
            case Int -> f.setInt(slot, ((Integer) boxed).intValue());
            case Long -> f.setLong(slot, ((Long) boxed).longValue());
            case Float -> f.setFloat(slot, ((Float) boxed).floatValue());
            case Double -> f.setDouble(slot, ((Double) boxed).doubleValue());
            case Boolean -> f.setBoolean(slot, ((Boolean) boxed).booleanValue());
            case Object -> f.setObject(slot, boxed);
            default -> throw new FrontendException("cannot write kind " + kind + " (WS F)");
        }
    }
}
