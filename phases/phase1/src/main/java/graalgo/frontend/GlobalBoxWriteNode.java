package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Writes an address-taken package-level variable (P3-M9, WS D).
 *
 * <p>The globals-frame slot holds a {@link GoBox}; the write stores the
 * canonical boxed value (after Go's value-copy rule) in the cell, so it
 * is observable through every pointer to the variable. Mirrors
 * {@link BoxWriteNode} but targets the program's globals frame.
 */
public final class GlobalBoxWriteNode extends GoStmtNode {
    private final MaterializedFrame globals;
    private final int slot;
    private final GoKind kind;
    @Child private GoExprNode value;

    public GlobalBoxWriteNode(GoKind kind, MaterializedFrame globals, int slot, GoExprNode value) {
        this.kind = kind;
        this.globals = globals;
        this.slot = slot;
        this.value = value;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object v = Boxing.copyForAssign(kind, Boxing.box(kind, value, frame));
        GoBox cell = (GoBox) globals.getObject(slot);
        if (cell == null) {
            throw new IllegalStateException("GlobalBoxWriteNode: null cell in globals slot " + slot
                    + " (global initializer did not run)");
        }
        cell.value = v;
    }
}
