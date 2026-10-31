package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code cap} builtin (WS G; verifies GG-0020).
 *
 * <p>Returns the capacity of an array or slice as a Go {@code int} (INT64).
 * Channel arguments never reach this node: the exporter lowers
 * {@code cap(ch)} to the {@code chancap} wire kind, built by
 * {@code ChanLenCapNode} on the {@code GoChan.capacity()} mechanism
 * (R-07: the old "channels are Phase 2" message was stale — channels
 * shipped in Phase 3). A {@code CHAN} argument here is therefore a
 * wire-shape error, not a scope gap.
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
            default -> throw new FrontendException(
                    "cap of " + argKind + " is not supported by CapNode (WS G);"
                            + " channel arguments must be lowered to chancap by the exporter");
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
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
