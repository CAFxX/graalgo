package graalgo.p3builtin.nodes;

import graalgo.sched.SchedulerCore;

import java.util.concurrent.atomic.AtomicInteger;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code runtime.GOMAXPROCS(n)}: the {@code "runtimegomaxprocs"} wire node.
 *
 * <p>The synthetic {@code runtime} overlay package declares
 * {@code GOMAXPROCS}; the frontend lowers the call to this additive wire
 * node (same pattern as {@code "runtimenumcpu"}). No Phase-1
 * {@code NodeFactory} change.
 *
 * <p>Sets the GOMAXPROCS value and returns the previous one, matching gc
 * semantics. With a scheduler installed (every launcher run) the value
 * IS the scheduler's: {@link SchedulerCore#setGomaxprocs} changes the
 * in-flight cap (SCHED-0003) and the launcher seeds it from the
 * {@code GOMAXPROCS} environment variable. Without a scheduler (bare AST
 * runs) a process-wide cell keeps save/restore round-trips correct.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class GoMaxProcsNode extends GoExprNode {

    /** The current GOMAXPROCS value; initialized to the host CPU count. */
    private static final AtomicInteger CURRENT =
            new AtomicInteger(Runtime.getRuntime().availableProcessors());

    /**
     * Returns the current GOMAXPROCS value — the same value Go code
     * observes through {@code runtime.GOMAXPROCS(0)}. Used by
     * {@code graalgo.p3link.SyncRuntime#procPin} to keep the returned
     * P-id inside {@code [0, GOMAXPROCS)}, the invariant real
     * {@code sync.Pool.pinSlow} relies on when sizing its per-P array.
     */
    public static int current() {
        SchedulerCore sc = SchedulerCore.installedOrNull();
        return sc != null ? sc.requestedCap() : CURRENT.get();
    }

    @Child private GoExprNode n;

    private GoMaxProcsNode(GoKind kind, GoExprNode n) {
        super(kind);
        this.n = n;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        int want = (int) n.executeLong(frame);
        SchedulerCore sc = SchedulerCore.installedOrNull();
        if (sc != null) {
            return sc.setGomaxprocs(want);
        }
        if (want > 0) {
            return CURRENT.getAndSet(want);
        }
        return CURRENT.get();
    }

    /**
     * Builds the node from the {@code "runtimegomaxprocs"} wire node.
     * Fields: {@code x} (the n expression), {@code type} (result
     * type-table id — always an int kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimegomaxprocs");
        String path = "runtimegomaxprocs.fields";
        Json.Value xv = f.opt("x");
        if (!(xv instanceof Json.Object o)) {
            throw new FrontendException(path + ": runtimegomaxprocs needs an \"x\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimegomaxprocs needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind kind = NodeFactory.kindOf(ctx, typeId, path);
        if (!kind.isIntKind()) {
            throw new FrontendException(
                    path + ": runtimegomaxprocs of non-int kind " + kind);
        }
        return new GoMaxProcsNode(kind, factory.buildExpr(o, ctx));
    }
}
