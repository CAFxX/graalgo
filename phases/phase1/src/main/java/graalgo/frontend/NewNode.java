package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStructDescriptor;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code new} builtin (WS G; verifies GG-0020).
 *
 * <p>{@code new(T)} allocates the zero value of {@code T} and returns a
 * pointer to it. Explicit {@code &x} / {@code *p} remain WS D scope errors;
 * this node is only built for the {@code new} builtin call.
 */
public final class NewNode extends GoExprNode {
    private final GoKind elemKind;
    private final GoStructDescriptor structDesc; // non-null iff elemKind == STRUCT

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc) {
        super(GoKind.POINTER);
        this.elemKind = elemKind;
        this.structDesc = structDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // P3-M8b: each new() must allocate a FRESH referent object. Boxing.zero
        // returns cached shared values (e.g. Long.valueOf(0L)) — if two new(int)
        // calls share the same referent, unsafe.Pointer interning (identity-based)
        // would give them the same handle, breaking p1 != q1 for distinct allocations.
        Object referent = freshZero(elemKind, structDesc);
        return new GoPointer(referent);
    }

    /** Fresh (unshared) zero value for new() allocations. */
    private static Object freshZero(GoKind kind, GoStructDescriptor structDesc) {
        return switch (kind.slotKind()) {
            case Int -> new int[1];
            case Long -> new long[1];
            case Float -> new float[1];
            case Double -> new double[1];
            case Boolean -> new boolean[1];
            case Object -> Boxing.zero(kind, structDesc);
            default -> throw new FrontendException("no zero for kind " + kind + " (WS E)");
        };
    }
}
