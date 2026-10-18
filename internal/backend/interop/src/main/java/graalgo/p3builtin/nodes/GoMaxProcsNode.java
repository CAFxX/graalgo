package graalgo.p3builtin.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.sched.Gomaxprocs;
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
 * semantics. The value is tracked per-process in {@link Gomaxprocs}; a
 * positive {@code n} also moves the installed scheduler's effective
 * in-flight cap (R-2 — the change is real, not bookkeeping).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class GoMaxProcsNode extends GoExprNode {

    /**
     * Returns the current GOMAXPROCS value — the same value Go code
     * observes through {@code runtime.GOMAXPROCS(0)}. Used by
     * {@code graalgo.p3link.SyncRuntime#procPin} to size the pin-id pool
     * so the returned P-id stays inside {@code [0, GOMAXPROCS)}, the
     * invariant real {@code sync.Pool.pinSlow} relies on when sizing its
     * per-P array.
     */
    public static int current() {
        return Gomaxprocs.current();
    }

    /**
     * Startup sync from the launcher {@code --gmp} flag. (The
     * {@link graalgo.sched.SchedulerCore} constructor performs the same
     * sync, so launchers that build the scheduler with the flag value
     * need no explicit call; this entry point exists for flows that
     * publish the value without constructing a scheduler.)
     */
    public static void setInitial(int gmp) {
        Gomaxprocs.setInitial(gmp);
    }

    @Child private GoExprNode n;

    private GoMaxProcsNode(GoKind kind, GoExprNode n) {
        super(kind);
        this.n = n;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        int want = (int) n.executeLong(frame);
        if (want > 0) {
            // R-2: publish the new value and move the installed
            // scheduler's effective cap (no-op when no scheduler is
            // installed); return the previous value, gc parity.
            return Gomaxprocs.set(want);
        }
        return Gomaxprocs.current();
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
