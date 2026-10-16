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
 * {@code int} (INT64). A channel argument dispatches to
 * {@link ChanOps#cap} (DIV-M3-1: timer/ticker channels always report 0 via
 * {@code TimerChan}; a nil channel reports 0, gc parity).
 */
public final class CapNode extends GoExprNode {
    @Child private GoExprNode child;
    private final GoKind argKind;

    public CapNode(GoExprNode child) {
        super(GoKind.INT64);
        this.child = child;
        this.argKind = child.kind();
        switch (argKind) {
            // R1-P1-1: CHAN dispatches to ChanOps.cap (channels are
            // implemented; the Phase-1 scope error was stale).
            case ARRAY, SLICE, CHAN -> {}
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
            // R1-P1-1: cap(ch) — buffer capacity via the runtime module's
            // ChanOps (call-through, never reimplemented). A nil channel
            // is 0 (gc parity, same as ChanLenCapNode).
            case CHAN -> {
                Object o = child.executeObject(frame);
                yield o == null ? 0 : ChanOps.cap((ChanOps.Chan) o);
            }
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
