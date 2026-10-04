package graalgo.frontend;

import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * Assigns a value to a package-level variable's globals-frame slot (M7).
 *
 * <p>Mirrors {@link AssignNode}'s typed writes (D5), including Go's
 * value-copy rule for struct/array values ({@link Boxing#copyForAssign}),
 * but targets the program's globals frame instead of the current frame.
 */
public final class GlobalWriteNode extends GoStmtNode {
    private final MaterializedFrame globals;
    private final int slot;
    private final GoKind kind;
    @Child private GoExprNode value;

    public GlobalWriteNode(GoKind kind, MaterializedFrame globals, int slot, GoExprNode value) {
        this.kind = kind;
        this.globals = globals;
        this.slot = slot;
        this.value = value;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        switch (kind.slotKind()) {
            case Int -> globals.setInt(slot, value.executeInt(frame));
            case Long -> globals.setLong(slot, value.executeLong(frame));
            case Float -> globals.setFloat(slot, value.executeFloat(frame));
            case Double -> globals.setDouble(slot, value.executeDouble(frame));
            case Boolean -> globals.setBoolean(slot, value.executeBoolean(frame));
            case Object -> globals.setObject(slot,
                    Boxing.copyForAssign(kind, value.executeObject(frame)));
        }
    }
}
