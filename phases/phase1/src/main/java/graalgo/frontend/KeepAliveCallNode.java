package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GcInterop;
import graalgo.values.GoKind;

/**
 * A call to {@code runtime.KeepAlive(x)} (P2-M4, GC interop — ADR-0002 D4;
 * via the synthetic {@code runtime} import overlay).
 *
 * <p>Evaluates the argument, then delegates to {@link GcInterop#keepAlive}.
 * In Phase 1 the target is the stub (throws); Phase 2 shadows it with the
 * {@code Reference.reachabilityFence}-based implementation.
 */
public final class KeepAliveCallNode extends GoExprNode {
    @Children private final GoExprNode[] args;

    public KeepAliveCallNode(GoExprNode[] args) {
        super(GoKind.INTERFACE);
        this.args = args;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object x = args[0].executeObject(frame);
        GcInterop.keepAlive(x);
        return null;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        executeObject(frame);
        return 0;
    }
}
