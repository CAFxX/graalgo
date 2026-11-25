package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * Declares a local variable: writes the initializer (or Go's zero value,
 * supplied by the factory as a {@link ConstNode}) to its frame slot,
 * using the slot kind's typed accessor (D5).
 */
public final class VarDeclNode extends GoStmtNode {
    private final int slot;
    private final GoKind kind;
    @Child private GoExprNode init;

    public VarDeclNode(int slot, GoKind kind, GoExprNode init) {
        this.slot = slot;
        this.kind = kind;
        this.init = init;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        switch (kind.slotKind()) {
            case Int -> frame.setInt(slot, init.executeInt(frame));
            case Long -> frame.setLong(slot, init.executeLong(frame));
            case Float -> frame.setFloat(slot, init.executeFloat(frame));
            case Double -> frame.setDouble(slot, init.executeDouble(frame));
            case Boolean -> frame.setBoolean(slot, init.executeBoolean(frame));
            // Go's value-copy rule for struct/array values (M4, WS E).
            case Object -> frame.setObject(slot, Boxing.copyForAssign(kind, init.executeObject(frame)));
        }
    }
}
