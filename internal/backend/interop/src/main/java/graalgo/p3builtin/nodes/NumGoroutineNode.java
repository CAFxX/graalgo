package graalgo.p3builtin.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code runtime.NumGoroutine()}: the {@code "runtimenumgoroutine"} wire node.
 *
 * <p>The synthetic {@code runtime} overlay package declares
 * {@code NumGoroutine}; the frontend lowers the call to this additive wire
 * node (same pattern as {@code "runtimenumcpu"}). No Phase-1
 * {@code NodeFactory} change.
 *
 * <p>Returns the number of goroutines that currently exist, matching gc's
 * {@code runtime.NumGoroutine} semantics ("returns the number of
 * goroutines that currently exist", {@code src/runtime/debug.go}): running,
 * runnable, and blocked goroutines (channel/mutex/timer waits, scheduler
 * parks) all count. The main goroutine counts — a fresh program observes 1.
 * The value is the scheduler's live-goroutine counter; see
 * {@code graalgo.sched.SchedulerCore.numGoroutines()} for the exact
 * existence definition (spawn-to-body-completion).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class NumGoroutineNode extends GoExprNode {

    private NumGoroutineNode(GoKind kind) {
        super(kind);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return graalgo.sched.SchedulerCore.current().numGoroutines();
    }

    /**
     * Builds the node from the {@code "runtimenumgoroutine"} wire node. Fields:
     * {@code type} (result type-table id — always an int kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "runtimenumgoroutine");
        String path = "runtimenumgoroutine.fields";
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimenumgoroutine needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind kind = NodeFactory.kindOf(ctx, typeId, path);
        if (!kind.isIntKind()) {
            throw new FrontendException(
                    path + ": runtimenumgoroutine of non-int kind " + kind);
        }
        return new NumGoroutineNode(kind);
    }
}
