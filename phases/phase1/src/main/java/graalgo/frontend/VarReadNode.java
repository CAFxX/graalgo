package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;

/**
 * Reads a local variable (frame slot by index; see {@link NodeFactory}).
 *
 * <p>Int carriers hold the value sign/zero-extended to 32 bits (D5); the
 * read extends to 64 bits with the same signedness, so parents never need
 * wrapper nodes.
 */
public final class VarReadNode extends GoExprNode {
    private final int slot;

    public VarReadNode(GoKind kind, int slot) {
        super(kind);
        this.slot = slot;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (kind()) {
            case INT8, INT16, INT32 -> (long) frame.getInt(slot);
            case UINT8, UINT16, UINT32 -> Integer.toUnsignedLong(frame.getInt(slot));
            // P3-M8b: UNSAFE_POINTER values are long handles (D5).
            case INT64, UINT64, UNTYPED_INT, UNSAFE_POINTER -> frame.getLong(slot);
            case FLOAT32 -> (long) frame.getFloat(slot);
            case FLOAT64, UNTYPED_FLOAT -> (long) frame.getDouble(slot);
            default -> throw new UnsupportedOperationException("cannot read " + kind() + " as long");
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return frame.getInt(slot);
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return frame.getFloat(slot);
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return switch (kind()) {
            case FLOAT32 -> (double) frame.getFloat(slot);
            default -> frame.getDouble(slot);
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return frame.getBoolean(slot);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return frame.getObject(slot);
    }
}
