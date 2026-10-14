package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * Reads a variable captured by a function literal from an ancestor frame
 * (M5, WS F).
 *
 * <p>Every function-literal frame has a {@code $closure} slot (slot 0)
 * holding the captured parent frame, installed by {@link GoFuncRootNode}
 * on entry. {@code depth} counts how many levels up the variable lives;
 * {@code slot} is the variable's slot index in that ancestor's frame.
 * The read uses the ancestor slot's kind (Int/Long/Float/Double/Boolean/
 * Object), mirroring {@link VarReadNode}'s extension rules.
 */
public final class CapReadNode extends GoExprNode {
    private final int depth;
    private final int slot;
    private final int closureSlot;

    public CapReadNode(GoKind kind, int depth, int slot, int closureSlot) {
        super(kind);
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
    public long executeLong(VirtualFrame frame) {
        MaterializedFrame f = targetFrame(frame);
        return switch (kind()) {
            case INT8, INT16, INT32 -> (long) f.getInt(slot);
            case UINT8, UINT16, UINT32 -> Integer.toUnsignedLong(f.getInt(slot));
            case INT64, UINT64, UNTYPED_INT -> f.getLong(slot);
            case FLOAT32 -> (long) f.getFloat(slot);
            case FLOAT64, UNTYPED_FLOAT -> (long) f.getDouble(slot);
            default -> throw new UnsupportedOperationException("cannot read " + kind() + " as long");
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return targetFrame(frame).getInt(slot);
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return targetFrame(frame).getFloat(slot);
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        MaterializedFrame f = targetFrame(frame);
        return switch (kind()) {
            case FLOAT32 -> (double) f.getFloat(slot);
            default -> f.getDouble(slot);
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return targetFrame(frame).getBoolean(slot);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return targetFrame(frame).getObject(slot);
    }
}
