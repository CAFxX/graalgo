package graalgo.p3builtin.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoStmtNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;

/**
 * {@code runtime.Gosched()}: the {@code "runtimegosched"} wire node
 * (statement kind).
 *
 * <p>The synthetic {@code runtime} overlay package declares
 * {@code Gosched}; the frontend lowers the call to this additive wire
 * node (same pattern as {@code "runtimenumgoroutine"}, but a statement —
 * Gosched returns no value). No Phase-1 {@code NodeFactory} change.
 *
 * <p>Semantics match gc's {@code runtime.Gosched} ({@code src/runtime/proc.go}):
 * "yields the processor, allowing other goroutines to run. It does not
 * suspend the current goroutine, so execution resumes automatically."
 * Implemented via {@link graalgo.sched.SchedulerCore#yieldCurrent}, the
 * scheduler's voluntary-yield primitive — the same mechanism as quantum
 * expiry (P2-D2): the yielding goroutine is re-admitted through the run
 * lane behind already-runnable goroutines, then resumes automatically.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerStmtKind} hook.
 */
public final class GoschedNode extends GoStmtNode {

    private GoschedNode() {
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        graalgo.sched.SchedulerCore.current().yieldCurrent();
    }

    /**
     * Builds the node from the {@code "runtimegosched"} wire node. No fields.
     */
    public static GoStmtNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        return new GoschedNode();
    }
}
