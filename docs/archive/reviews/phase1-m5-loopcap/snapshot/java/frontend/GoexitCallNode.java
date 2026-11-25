package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Goexit;

/**
 * A call to {@code runtime.Goexit()} (M5, WS F).
 *
 * <p>Terminates the goroutine: runs deferred calls (not recoverable), then
 * unwinds as a {@link Goexit} control-flow exception. In Phase 1 the only
 * goroutine is main, so the launcher reports gc's fatal deadlock
 * ({@code no goroutines (main called runtime.Goexit)}, exit 2); the exit-0
 * termination applies to non-main goroutines (Phase 2).
 */
public final class GoexitCallNode extends GoExprNode {
    public GoexitCallNode() {
        super(graalgo.values.GoKind.INTERFACE);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Goexit.goexit();
        throw new AssertionError("unreachable");
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        executeObject(frame);
        throw new AssertionError("unreachable");
    }
}
