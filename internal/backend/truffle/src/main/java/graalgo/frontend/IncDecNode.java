package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;

/**
 * {@code x++} / {@code x--}: read-modify-write on a frame slot.
 *
 * <p>Go allows {@code ++}/{@code --} on float <em>and complex</em>
 * variables too ({@code f++} ≡ {@code f += 1}; {@code c++} adds
 * {@code 1+0i}); integer slots re-truncate to the width (wraparound,
 * matching Go).
 */
public final class IncDecNode extends GoStmtNode {
    private final int slot;
    private final GoKind kind;
    private final long delta;

    public IncDecNode(int slot, GoKind kind, long delta) {
        if (!kind.isIntKind() && !kind.isFloatKind() && !kind.isComplexKind()) {
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
        // PKG-026: complex ++/-- adds delta to the real part (Go: c++ ≡ c += 1).
        if (kind.isComplexKind()) {
            Object v = frame.getObject(slot);
            if (v instanceof graalgo.values.GoComplex32 c) {
                frame.setObject(slot, new graalgo.values.GoComplex32(c.re() + (float) delta, c.im()));
            } else if (v instanceof graalgo.values.GoComplex64 c) {
                frame.setObject(slot, new graalgo.values.GoComplex64(c.re() + delta, c.im()));
            } else {
                throw new FrontendException("++/-- on non-complex value " + v + " (PKG-026)");
            }
            return;
        }
        switch (kind.slotKind()) {
            case Int -> frame.setInt(slot, extendInt(frame.getInt(slot) + (int) delta, kind));
            case Long -> frame.setLong(slot, frame.getLong(slot) + delta);
            case Float -> frame.setFloat(slot, frame.getFloat(slot) + delta);
            case Double -> frame.setDouble(slot, frame.getDouble(slot) + delta);
            default -> throw new UnsupportedOperationException("++/-- on " + kind);
        }
    }
}
