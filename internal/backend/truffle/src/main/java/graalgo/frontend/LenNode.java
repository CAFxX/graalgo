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
 * channel as a Go {@code int} (INT64). The child is evaluated with the
 * execute method matching its kind. A channel argument delegates to
 * {@link ChanOps#len}, which reports the buffered element count; a nil
 * channel reports 0 (gc parity — len of a nil channel does not block).
 * {@code TimerChan} masking is preserved automatically: {@code ChanOps.len}
 * calls {@code GoChan.buffered()}, which {@code TimerChan} overrides to 0
 * (DIV-M3-1), so timer/ticker channels always observe len 0.
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
            // REVIEW-2026-11-03-16 P0-2 (L-B): len(ch) is a valid program;
            // channels are no longer a scope error. Delegates to ChanOps
            // (TimerChan masking preserved, see class javadoc).
            case CHAN -> {}
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
                Object operand = child.executeObject(frame);
                GoSlice s = (GoSlice) operand;
                yield s == null ? 0 : s.len();
            }
            case MAP -> {
                GoMap m = (GoMap) child.executeObject(frame);
                yield m == null ? 0 : m.size();
            }
            case CHAN -> {
                Object operand = child.executeObject(frame);
                if (operand == null) {
                    yield 0;
                }
                if (!(operand instanceof ChanOps.Chan c)) {
                    throw new IllegalStateException("len of channel: not a channel value ("
                            + operand.getClass().getName() + ")");
                }
                yield ChanOps.len(c);
            }
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
