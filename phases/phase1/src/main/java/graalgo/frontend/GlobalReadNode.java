package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;

/**
 * Reads a package-level variable (M7).
 *
 * <p>Package-level variables live in a dedicated globals frame, allocated
 * once per program by {@link NodeFactory#buildProgram} and initialized at
 * the top of {@code func main} in dependency order. Every function —
 * including function literals, which never capture globals — reads the
 * same cell. The typed reads mirror {@link VarReadNode}'s extension rules.
 */
public final class GlobalReadNode extends GoExprNode {
    private final MaterializedFrame globals;
    private final int slot;

    public GlobalReadNode(GoKind kind, MaterializedFrame globals, int slot) {
        super(kind);
        this.globals = globals;
        this.slot = slot;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (kind()) {
            case INT8, INT16, INT32 -> (long) globals.getInt(slot);
            case UINT8, UINT16, UINT32 -> Integer.toUnsignedLong(globals.getInt(slot));
            case INT64, UINT64, UNTYPED_INT -> globals.getLong(slot);
            case FLOAT32 -> (long) globals.getFloat(slot);
            case FLOAT64, UNTYPED_FLOAT -> (long) globals.getDouble(slot);
            default -> throw new UnsupportedOperationException("cannot read " + kind() + " as long");
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return globals.getInt(slot);
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return globals.getFloat(slot);
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return switch (kind()) {
            case FLOAT32 -> (double) globals.getFloat(slot);
            default -> globals.getDouble(slot);
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return globals.getBoolean(slot);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return globals.getObject(slot);
    }
}
