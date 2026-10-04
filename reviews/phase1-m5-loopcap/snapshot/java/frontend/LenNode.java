package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code len} builtin (WS G; verifies GG-0020).
 *
 * <p>Returns the length of a string (in bytes), array, slice, or map as a
 * Go {@code int} (INT64). The child is evaluated with the execute method
 * matching its kind. A channel argument is a Phase-2 scope error (no
 * channels in Phase 1); the factory rejects it before building this node.
 */
public final class LenNode extends GoExprNode {
    @Child private GoExprNode child;
    private final GoKind argKind;

    public LenNode(GoExprNode child) {
        super(GoKind.INT64);
        this.child = child;
        this.argKind = child.kind();
        switch (argKind) {
            case STRING, ARRAY, SLICE, MAP -> {}
            default -> throw new FrontendException(
                    "len of " + argKind + " is not implemented (WS G); channels are Phase 2");
        }
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (argKind) {
            case STRING -> ((GoString) child.executeObject(frame)).bytes().length;
            case ARRAY -> java.lang.reflect.Array.getLength(child.executeObject(frame));
            case SLICE -> ((GoSlice) child.executeObject(frame)).len();
            case MAP -> ((GoMap) child.executeObject(frame)).size();
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
