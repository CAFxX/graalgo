package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Panic;

/**
 * The {@code recover()} builtin (M5, WS F).
 *
 * <p>Returns the in-flight panic value, or Go nil if none. Non-nil only
 * when called directly by a deferred function during panic unwind (the
 * {@link graalgo.runtime.DeferChain} arms the recover-allowed flag around
 * each deferred call; {@link FuncValueCallNode} clears it for nested
 * calls). Defined behavior elsewhere = nil (gc).
 *
 * <p>Minimal WS-F wiring: worker-G's builtin surface (GG-0020) absorbs this
 * node when it lands; the runtime call ({@link Panic#recover}) is the
 * frozen interface and does not change.
 */
public final class RecoverBuiltinNode extends GoExprNode {
    public RecoverBuiltinNode() {
        super(graalgo.values.GoKind.INTERFACE);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return Panic.recover();
    }
}
