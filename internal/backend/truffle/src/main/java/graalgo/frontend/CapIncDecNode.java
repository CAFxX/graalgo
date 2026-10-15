package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * {@code x++} / {@code x--} on a variable captured by a function literal
 * (M5, WS F). Read-modify-write through the {@code $closure} chain, with
 * the same width-truncation rules as {@link IncDecNode}.
 */
public final class CapIncDecNode extends GoStmtNode {
    private final GoKind kind;
    private final int depth;
    private final int slot;
    private final int closureSlot;
    private final long delta;

    public CapIncDecNode(GoKind kind, int depth, int slot, int closureSlot, long delta) {
        if (!kind.isIntKind() && !kind.isFloatKind() && !kind.isComplexKind()) {
            throw new IllegalArgumentException("++/-- requires a numeric kind, got " + kind);
        }
        this.kind = kind;
        this.depth = depth;
        this.slot = slot;
        this.closureSlot = closureSlot;
        this.delta = delta;
    }

    private MaterializedFrame targetFrame(VirtualFrame frame) {
        MaterializedFrame f = (MaterializedFrame) frame.getObject(closureSlot);
        for (int i = 0; i < depth; i++) {
            f = (MaterializedFrame) f.getObject(closureSlot);
        }
        return f;
    }

    private static int extendInt(int masked, GoKind kind) {
        return switch (kind) {
            case INT8 -> (int) (byte) masked;
            case INT16 -> (int) (short) masked;
            case UINT8 -> masked & 0xFF;
            case UINT16 -> masked & 0xFFFF;
            default -> masked;
        };
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        MaterializedFrame f = targetFrame(frame);
        // PKG-026: complex ++/-- adds delta to the real part (mirrors IncDecNode).
        if (kind.isComplexKind()) {
            Object v = f.getObject(slot);
            if (v instanceof graalgo.values.GoComplex32 c) {
                f.setObject(slot, new graalgo.values.GoComplex32(c.re() + (float) delta, c.im()));
            } else if (v instanceof graalgo.values.GoComplex64 c) {
                f.setObject(slot, new graalgo.values.GoComplex64(c.re() + delta, c.im()));
            } else {
                throw new FrontendException("++/-- on non-complex value " + v + " (PKG-026)");
            }
            return;
        }
        switch (kind.slotKind()) {
            case Int -> f.setInt(slot, extendInt(f.getInt(slot) + (int) delta, kind));
            case Long -> f.setLong(slot, f.getLong(slot) + delta);
            case Float -> f.setFloat(slot, f.getFloat(slot) + delta);
            case Double -> f.setDouble(slot, f.getDouble(slot) + delta);
            default -> throw new UnsupportedOperationException("++/-- on " + kind);
        }
    }
}
