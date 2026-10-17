package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

// verifies: GG-0021 (WS G builtin)
/**
 * The {@code clear} builtin (WS G; verifies GG-0021).
 *
 * <p>{@code clear(m)} deletes all entries from map {@code m}.
 * {@code clear(s)} zeroes all elements of slice {@code s} (up to its length).
 * It is a statement-only builtin (no result): {@link #executeLong} is
 * implemented so {@link ExprStmtNode} can execute it.
 *
 * <p>Slice zeroing goes through {@link Boxing#clearSlice}, the single
 * canonical implementation covering every D5 backing carrier
 * ({@code byte[]/short[]/int[]/long[]/float[]/double[]/boolean[]},
 * complex-typed arrays, and {@code Object[]} with the Go zero value per
 * element kind).
 */
public final class ClearNode extends GoExprNode {
    @Child private GoExprNode targetChild;
    /**
     * The slice's element kind (for the {@code Object[]} zero value).
     * {@link GoKind#INTERFACE} when unknown — degrades to nulling, the
     * pre-fix behavior.
     */
    private final GoKind elemKind;

    public ClearNode(GoExprNode targetChild) {
        this(targetChild, GoKind.INTERFACE);
    }

    /** Builds with the slice's element kind resolved from the type table. */
    public ClearNode(GoExprNode targetChild, GoKind elemKind) {
        // No result; INT64 is a placeholder kind (only executeLong is
        // ever called, via ExprStmtNode).
        super(GoKind.INT64);
        this.targetChild = targetChild;
        this.elemKind = elemKind;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object target = targetChild.executeObject(frame);
        GoKind k = targetChild.kind();
        if (k == GoKind.MAP) {
            // clear on a nil map is a no-op (gc parity).
            if (target != null) {
                ((GoMap) target).clear();
            }
        } else if (k == GoKind.SLICE) {
            GoSlice gs = (GoSlice) target;
            // clear on a nil slice is a no-op (GoSlice is null then).
            if (gs != null) {
                Boxing.clearSlice(gs, elemKind, null);
            }
        }
        return 0L;
    }
}
