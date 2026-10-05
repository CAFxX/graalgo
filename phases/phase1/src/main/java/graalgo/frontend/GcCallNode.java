package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GcInterop;
import graalgo.values.GoKind;

/**
 * A call to {@code runtime.GC()} (P2-M4, GC interop — ADR-0002 D4; via the
 * synthetic {@code runtime} import overlay).
 *
 * <p>Delegates to {@link GcInterop#gc}. In Phase 1 the target is the stub
 * (throws); Phase 2 shadows it with the {@code System.gc()} + MXBean-poll
 * implementation.
 */
public final class GcCallNode extends GoExprNode {
    public GcCallNode() {
        super(GoKind.INTERFACE);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GcInterop.gc();
        return null;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        executeObject(frame);
        return 0;
    }
}
