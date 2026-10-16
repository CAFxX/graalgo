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
 * <p>Returns the capacity of an array, slice, or channel (buffer size) as
 * a Go {@code int} (INT64). A channel argument dispatches to
 * {@link ChanOps#cap} (the same call the P3 {@code "chancap"} wire node
 * makes); a nil channel reports 0 (gc parity — cap of a nil channel is
 * not a blocking operation).
 */
public final class CapNode extends GoExprNode {
    @Child private GoExprNode child;
    private final GoKind argKind;

    public CapNode(GoExprNode child) {
        super(GoKind.INT64);
        this.child = child;
        this.argKind = child.kind();
        switch (argKind) {
            // P3-M5: channels are implemented (WS E); cap(ch) dispatches to
            // ChanOps.cap — the "channels are Phase 2" rejection is stale.
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
            // P3-M5: cap(ch) — buffer capacity. Nil channel reports 0
            // (gc parity); the cast is fail-fast on an exporter bug.
            case CHAN -> {
                Object o = child.executeObject(frame);
                if (o == null) {
                    yield 0;
                }
                yield ChanOps.cap((ChanOps.Chan) o);
            }
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
