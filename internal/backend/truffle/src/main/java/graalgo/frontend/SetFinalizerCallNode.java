package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GcInterop;
import graalgo.values.GoKind;

/**
 * A call to {@code runtime.SetFinalizer(obj, finalizer)} (P2-M4, GC interop —
 * ADR-0002 D4; via the synthetic {@code runtime} import overlay).
 *
 * <p>Evaluates both arguments, then delegates to {@link GcInterop#setFinalizer}.
 * In Phase 1 the target is the stub (throws); Phase 2 shadows it with the
 * {@code Cleaner}-based implementation.
 */
public final class SetFinalizerCallNode extends GoExprNode {
    @Children private final GoExprNode[] args;

    public SetFinalizerCallNode(GoExprNode[] args) {
        super(GoKind.INTERFACE);
        this.args = args;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object obj = args[0].executeObject(frame);
        Object finalizer = args[1].executeObject(frame);
        GcInterop.setFinalizer(obj, finalizer);
        return null;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        executeObject(frame);
        return 0;
    }
}
