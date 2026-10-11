package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Reads an address-taken package-level variable (P3-M9, WS D).
 *
 * <p>The globals-frame slot holds a {@link GoBox}; the value is read from
 * the box in canonical boxed form and unboxed with the variable's kind,
 * mirroring {@link BoxReadNode}'s extension rules.
 */
public final class GlobalBoxReadNode extends GoExprNode {
    private final MaterializedFrame globals;
    private final int slot;

    public GlobalBoxReadNode(GoKind kind, MaterializedFrame globals, int slot) {
        super(kind);
        this.globals = globals;
        this.slot = slot;
    }

    private Object boxed() {
        GoBox box = (GoBox) globals.getObject(slot);
        if (box == null) {
            throw new IllegalStateException("GlobalBoxReadNode: null cell in globals slot " + slot
                    + " (global initializer did not run)");
        }
        return box.value;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object v = boxed();
        return switch (kind()) {
            case INT8, INT16, INT32 -> (long) ((Number) v).intValue();
            case UINT8, UINT16, UINT32 -> ((Number) v).longValue() & 0xFFFFFFFFL;
            case INT64, UINT64, UNTYPED_INT, UNSAFE_POINTER -> ((Number) v).longValue();
            case FLOAT32 -> (long) ((Float) v).floatValue();
            case FLOAT64, UNTYPED_FLOAT -> (long) ((Double) v).doubleValue();
            default -> throw new UnsupportedOperationException("cannot read " + kind() + " as long");
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Integer) boxed()).intValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Float) boxed()).floatValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        Object v = boxed();
        return switch (kind()) {
            case FLOAT32 -> (double) ((Float) v).floatValue();
            default -> ((Double) v).doubleValue();
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return ((Boolean) boxed()).booleanValue();
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return boxed();
    }
}
