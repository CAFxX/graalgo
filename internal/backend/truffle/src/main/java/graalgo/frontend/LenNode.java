package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.ChanOps;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code len} builtin (WS G; verifies GG-0020).
 *
 * <p>Returns the length of a string (in bytes), array, slice, map, or
 * channel (buffered element count) as a Go {@code int} (INT64). The child
 * is evaluated with the execute method matching its kind. A channel
 * argument dispatches to {@link ChanOps#len} (the same call the P3
 * {@code "chanlen"} wire node makes); a nil channel reports 0 (gc parity
 * — len of a nil channel is not a blocking operation).
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
            // P3-M5: channels are implemented (WS E); len(ch) dispatches to
            // ChanOps.len — the "channels are Phase 2" rejection is stale.
            case STRING, UNTYPED_STRING, ARRAY, SLICE, MAP, CHAN -> {}
            // P3-M9: len(p) where p is *[N]T returns N (Go spec).
            case POINTER -> {}
            default -> throw new FrontendException(
                    "len of " + argKind + " is not implemented (WS G)");
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
                GoSlice s = (GoSlice) child.executeObject(frame);
                yield s == null ? 0 : s.len();
            }
            case MAP -> {
                GoMap m = (GoMap) child.executeObject(frame);
                yield m == null ? 0 : m.size();
            }
            // P3-M5: len(ch) — buffered element count. Nil channel reports
            // 0 (gc parity); the cast is fail-fast on an exporter bug.
            case CHAN -> {
                Object o = child.executeObject(frame);
                if (o == null) {
                    yield 0;
                }
                yield ChanOps.len((ChanOps.Chan) o);
            }
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
