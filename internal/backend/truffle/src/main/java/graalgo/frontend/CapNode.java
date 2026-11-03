package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.ChanOps;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code cap} builtin (WS G; verifies GG-0020).
 *
 * <p>Returns the capacity of an array, slice, or channel as a Go
 * {@code int} (INT64). A channel argument delegates to
 * {@link ChanOps#cap}, which reports the buffer capacity; a nil channel
 * reports 0 (gc parity — cap of a nil channel does not block).
 * {@code TimerChan} masking is preserved automatically: {@code ChanOps.cap}
 * calls {@code GoChan.capacity()}, which {@code TimerChan} overrides to 0
 * (DIV-M3-1), so timer/ticker channels always observe cap 0.
 */
public final class CapNode extends GoExprNode {
    @Child private GoExprNode child;
    private final GoKind argKind;

    public CapNode(GoExprNode child) {
        super(GoKind.INT64);
        this.child = child;
        this.argKind = child.kind();
        switch (argKind) {
            case ARRAY, SLICE -> {}
            // REVIEW-2026-11-03-16 P0-2 (L-B): cap(ch) is a valid program;
            // channels are no longer a scope error. Delegates to ChanOps
            // (TimerChan masking preserved, see class javadoc).
            case CHAN -> {}
            default -> throw new FrontendException(
                    "cap of " + argKind + " is not implemented (WS G)");
        }
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (argKind) {
            case ARRAY -> java.lang.reflect.Array.getLength(child.executeObject(frame));
            // M7: cap of a nil slice is 0 (Go spec, Length and capacity).
            case SLICE -> {
                GoSlice s = (GoSlice) child.executeObject(frame);
                yield s == null ? 0 : s.cap();
            }
            case CHAN -> {
                Object operand = child.executeObject(frame);
                if (operand == null) {
                    yield 0;
                }
                if (!(operand instanceof ChanOps.Chan c)) {
                    throw new IllegalStateException("cap of channel: not a channel value ("
                            + operand.getClass().getName() + ")");
                }
                yield ChanOps.cap(c);
            }
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
