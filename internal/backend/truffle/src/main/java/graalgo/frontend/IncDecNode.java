package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;

/**
 * {@code x++} / {@code x--}: read-modify-write on a frame slot.
 *
 * <p>Go allows {@code ++}/{@code --} on float variables too
 * ({@code f++} ≡ {@code f += 1}); integer slots re-truncate to the width
 * (wraparound, matching Go).
 */
public final class IncDecNode extends GoStmtNode {
    private final int slot;
    private final GoKind kind;
    private final long delta;

    public IncDecNode(int slot, GoKind kind, long delta) {
        if (!kind.isIntKind() && !kind.isFloatKind()) {
            throw new IllegalArgumentException("++/-- requires a numeric kind, got " + kind);
        }
        this.slot = slot;
        this.kind = kind;
        this.delta = delta;
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
        switch (kind.slotKind()) {
            case Int -> frame.setInt(slot, extendInt(frame.getInt(slot) + (int) delta, kind));
            case Long -> frame.setLong(slot, frame.getLong(slot) + delta);
            case Float -> frame.setFloat(slot, frame.getFloat(slot) + delta);
            case Double -> frame.setDouble(slot, frame.getDouble(slot) + delta);
            default -> throw new UnsupportedOperationException("++/-- on " + kind);
        }
    }
}
