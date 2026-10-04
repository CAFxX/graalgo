package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/** Assigns a value to a local variable's frame slot (typed write, D5). */
public final class AssignNode extends GoStmtNode {
    private final int slot;
    private final GoKind kind;
    private final boolean copyValue;
    @Child private GoExprNode value;

    public AssignNode(int slot, GoKind kind, GoExprNode value) {
        this(slot, kind, value, true);
    }

    /**
     * @param copyValue Go's value-copy rule for struct/array values
     *                  (M4, WS E); false for internal temporaries that must
     *                  alias the original (lvalue containers).
     */
    public AssignNode(int slot, GoKind kind, GoExprNode value, boolean copyValue) {
        this.slot = slot;
        this.kind = kind;
        this.value = value;
        this.copyValue = copyValue;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        switch (kind.slotKind()) {
            case Int -> frame.setInt(slot, value.executeInt(frame));
            case Long -> frame.setLong(slot, value.executeLong(frame));
            case Float -> frame.setFloat(slot, value.executeFloat(frame));
            case Double -> frame.setDouble(slot, value.executeDouble(frame));
            case Boolean -> frame.setBoolean(slot, value.executeBoolean(frame));
            case Object -> frame.setObject(slot,
                    copyValue ? Boxing.copyForAssign(kind, value.executeObject(frame))
                              : value.executeObject(frame));
        }
    }
}
