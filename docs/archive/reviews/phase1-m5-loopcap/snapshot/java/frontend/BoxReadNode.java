package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Reads a boxed (per-iteration capture, M6) variable.
 *
 * <p>The frame slot holds a {@link GoBox}; the value is read from the
 * box in canonical boxed form and unboxed with the variable's kind,
 * mirroring {@link VarReadNode}'s extension rules. Used for loop-body
 * reads of captured loop variables, for {@code for}-header bodies, and
 * for snapshot slots inside function literals (where the slot holds the
 * cell snapshotted at closure creation).
 */
public final class BoxReadNode extends GoExprNode {
    private final int slot;

    public BoxReadNode(GoKind kind, int slot) {
        super(kind);
        this.slot = slot;
    }

    private Object boxed(VirtualFrame frame) {
        GoBox box = (GoBox) frame.getObject(slot);
        if (box == null) {
            throw new IllegalStateException("BoxReadNode: null cell in slot " + slot
                    + " (per-iteration cell not installed)");
        }
        return box.value;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object v = boxed(frame);
        return switch (kind()) {
            case INT8, INT16, INT32 -> (long) ((Integer) v).intValue();
            case UINT8, UINT16, UINT32 -> Integer.toUnsignedLong((Integer) v);
            case INT64, UINT64, UNTYPED_INT -> ((Long) v).longValue();
            case FLOAT32 -> (long) ((Float) v).floatValue();
            case FLOAT64, UNTYPED_FLOAT -> (long) ((Double) v).doubleValue();
            default -> throw new UnsupportedOperationException("cannot read " + kind() + " as long");
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Integer) boxed(frame)).intValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Float) boxed(frame)).floatValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        Object v = boxed(frame);
        return switch (kind()) {
            case FLOAT32 -> (double) ((Float) v).floatValue();
            default -> ((Double) v).doubleValue();
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return ((Boolean) boxed(frame)).booleanValue();
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return boxed(frame);
    }
}
