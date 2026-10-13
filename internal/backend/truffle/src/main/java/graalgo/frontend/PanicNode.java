package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code panic} and {@code recover} builtins (WS G wiring, WS F
 * mechanism; verifies GG-0020).
 *
 * <p>{@code panic(x)} calls {@link Panic#throwPanic(Object)} (frozen
 * cross-workstream interface). {@code recover()} calls
 * {@link Panic#recover()}. Both are statement/expression builtins per Go
 * syntax: {@code panic} is statement-only (it never returns);
 * {@code recover} yields an {@code any}.
 */
public final class PanicNode extends GoExprNode {
    @Child private GoExprNode arg; // null for recover()

    /** panic(x). */
    public PanicNode(GoExprNode arg) {
        super(GoKind.INTERFACE); // never returns; placeholder kind
        this.arg = arg;
    }

    /** recover(). */
    public PanicNode() {
        super(GoKind.INTERFACE); // Go 'any'
        this.arg = null;
    }

    public boolean isRecover() {
        return arg == null;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (arg == null) {
            return Panic.recover();
        }
        Panic.throwPanic(readArg(arg, frame), null, null, this);
        throw new IllegalStateException("throwPanic returned");
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // For panic(x) in statement position: throws; never returns.
        executeObject(frame);
        throw new IllegalStateException("throwPanic returned");
    }

    private static Object readArg(GoExprNode child, VirtualFrame frame) {
        return switch (child.kind().slotKind()) {
            case Int -> child.executeInt(frame);
            case Long -> child.executeLong(frame);
            case Float -> child.executeFloat(frame);
            case Double -> child.executeDouble(frame);
            case Boolean -> child.executeBoolean(frame);
            case Object -> child.executeObject(frame);
            default -> throw new FrontendException("unsupported kind: " + child.kind());
        };
    }
}
