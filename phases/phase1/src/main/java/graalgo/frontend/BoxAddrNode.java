package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * Address of a boxed local variable, parameter, or named result:
 * {@code &x} where x's frame slot holds a {@link GoBox} cell (P3-M9,
 * WS D). The returned pointer addresses the cell itself, so reads and
 * writes through the pointer are observable through the variable and
 * vice versa (gc aliasing); a snapshot of the current value would
 * silently detach the pointer from the variable.
 */
public final class BoxAddrNode extends GoExprNode {
    private final int slot;

    public BoxAddrNode(int slot) {
        super(GoKind.POINTER);
        this.slot = slot;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoBox box = (GoBox) frame.getObject(slot);
        if (box == null) {
            throw new IllegalStateException("BoxAddrNode: null cell in slot " + slot
                    + " (boxed variable not yet declared)");
        }
        return new GoPointer(box);
    }
}
