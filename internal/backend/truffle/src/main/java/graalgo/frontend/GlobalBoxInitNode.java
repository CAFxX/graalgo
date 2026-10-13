package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Initializes an address-taken package-level variable (P3-M9, WS D).
 *
 * <p>The globals-frame slot holds a {@link GoBox}; the initializer creates
 * the cell once with the canonical boxed initial value (after Go's
 * value-copy rule), so every later access (via {@link GlobalBoxReadNode} /
 * {@link GlobalBoxWriteNode}) shares the same cell. Mirrors
 * {@link GlobalWriteNode} but installs a {@link GoBox} instead of a raw
 * value.
 */
public final class GlobalBoxInitNode extends GoStmtNode {
    private final MaterializedFrame globals;
    private final int slot;
    private final GoKind kind;
    @Child private GoExprNode value;

    public GlobalBoxInitNode(GoKind kind, MaterializedFrame globals, int slot, GoExprNode value) {
        this.kind = kind;
        this.globals = globals;
        this.slot = slot;
        this.value = value;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object v = Boxing.copyForAssign(kind, Boxing.box(kind, value, frame));
        globals.setObject(slot, new GoBox(v));
    }
}
