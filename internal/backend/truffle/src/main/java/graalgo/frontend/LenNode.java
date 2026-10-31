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
 * matching its kind. Channel arguments never reach this node: the exporter
 * lowers {@code len(ch)} to the {@code chanlen} wire kind, built by
 * {@code ChanLenCapNode} on the {@code GoChan.buffered()} mechanism
 * (R-07: the old "channels are Phase 2" message was stale — channels
 * shipped in Phase 3). A {@code CHAN} argument here is therefore a
 * wire-shape error, not a scope gap.
 */
public final class LenNode extends GoExprNode {
    @Child private GoExprNode child;
    private final GoKind argKind;

    public LenNode(GoExprNode child) {
        super(GoKind.INT64);
        this.child = child;
        this.argKind = child.kind();
        switch (argKind) {
            // M7: UNTYPED_STRING (e.g. len("héllo")) folds to a string.
            case STRING, UNTYPED_STRING, ARRAY, SLICE, MAP -> {}
            // P3-M9: len(p) where p is *[N]T returns N (Go spec).
            case POINTER -> {}
            default -> throw new FrontendException(
                    "len of " + argKind + " is not supported by LenNode (WS G);"
                            + " channel arguments must be lowered to chanlen by the exporter");
        }
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (argKind) {
            case STRING, UNTYPED_STRING -> ((GoString) child.executeObject(frame)).bytes().length;
            case ARRAY -> java.lang.reflect.Array.getLength(child.executeObject(frame));
            // P3-M9: len of *[N]T — dereference the pointer, get array length.
            case POINTER -> {
                Object o = child.executeObject(frame);
                if (o instanceof graalgo.values.GoPointer p) {
                    o = p.referent();
                }
                yield java.lang.reflect.Array.getLength(o);
            }
            // M7: len of a nil slice/map is 0 (Go spec, Length and capacity).
            case SLICE -> {
                Object operand = child.executeObject(frame);
                GoSlice s = (GoSlice) operand;
                yield s == null ? 0 : s.len();
            }
            case MAP -> {
                GoMap m = (GoMap) child.executeObject(frame);
                yield m == null ? 0 : m.size();
            }
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
