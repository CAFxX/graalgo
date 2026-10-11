package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * Address of an address-taken package-level variable: {@code &g} where
 * the globals-frame slot holds a {@link GoBox} cell (P3-M9, WS D). The
 * returned pointer addresses the cell itself, so {@code *p = v} is
 * observable through the variable and vice versa (gc aliasing). The
 * companion of {@link GlobalBoxReadNode} / {@link GlobalBoxWriteNode}.
 */
public final class GlobalBoxAddrNode extends GoExprNode {
    private final MaterializedFrame globals;
    private final int slot;

    public GlobalBoxAddrNode(MaterializedFrame globals, int slot) {
        super(GoKind.POINTER);
        this.globals = globals;
        this.slot = slot;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoBox box = (GoBox) globals.getObject(slot);
        if (box == null) {
            throw new IllegalStateException("GlobalBoxAddrNode: null cell in globals slot "
                    + slot + " (global initializer did not run)");
        }
        return new GoPointer(box);
    }
}
