package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * Declares a boxed (per-iteration capture, M6) variable.
 *
 * <p>Allocates a fresh {@link GoBox} holding the initializer (or Go's
 * zero value, supplied by the factory) and stores it in the variable's
 * frame slot. Because the declaration executes on every iteration, each
 * iteration — and every closure capturing the variable — gets its own
 * cell. The companion of {@link VarDeclNode} for boxed variables.
 */
public final class BoxDeclNode extends GoStmtNode {
    private final int slot;
    private final GoKind kind;
    @Child private GoExprNode init;

    public BoxDeclNode(int slot, GoKind kind, GoExprNode init) {
        this.slot = slot;
        this.kind = kind;
        this.init = init;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object v = Boxing.copyForAssign(kind, Boxing.box(kind, init, frame));
        frame.setObject(slot, new GoBox(v));
    }
}
