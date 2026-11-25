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
        return new GoPointer(Boxing.zero(elemKind, structDesc));
    }
}
