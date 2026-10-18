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
 *
 * <p>P1-23/24: for a pointer to an array ({@code len(p)}, {@code p *[N]T})
 * the length comes from the static type ({@code ptrArrayLen}), never from
 * the referent — gc returns N without dereferencing, so a nil pointer
 * yields N instead of faulting.
 */
public final class LenNode extends GoExprNode {
    @Child private GoExprNode child;
    private final GoKind argKind;
    /**
     * Static array length when {@code argKind == POINTER} (see class
     * javadoc); -1 for every other argument kind.
     */
    private final int ptrArrayLen;

    public LenNode(GoExprNode child, int ptrArrayLen) {
        super(GoKind.INT64);
        this.child = child;
        this.argKind = child.kind();
        this.ptrArrayLen = ptrArrayLen;
        switch (argKind) {
            // M7: UNTYPED_STRING (e.g. len("héllo")) folds to a string.
            case STRING, UNTYPED_STRING, ARRAY, SLICE, MAP -> {}
            // P1-23/24: len(p) where p is *[N]T returns N (Go spec). The
            // factory resolves N from the type table at construction; a
            // missing length is a broken factory, never a runtime guess.
            case POINTER -> {
                if (ptrArrayLen < 0) {
                    throw new FrontendException(
                            "len of pointer without a resolved array length (WS G)");
                }
            }
            default -> throw new FrontendException(
                    "len of " + argKind + " is not implemented (WS G); channels are Phase 2");
        }
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (argKind) {
            case STRING, UNTYPED_STRING -> ((GoString) child.executeObject(frame)).bytes().length;
            case ARRAY -> java.lang.reflect.Array.getLength(child.executeObject(frame));
            // P1-23/24: the length is static — no dereference, so nil
            // pointers return N exactly like gc. The operand is still
            // evaluated (Go evaluates builtin arguments; only the
            // dereference is skipped), preserving side effects.
            case POINTER -> {
                child.executeObject(frame);
                yield ptrArrayLen;
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
            default -> throw new IllegalStateException("unreachable: " + argKind);
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
