package graalgo.rtmemstats;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;

/**
 * {@code runtime.ReadMemStats(m)}: the {@code "runtimereadmemstats"} wire
 * node.
 *
 * <p>The synthetic {@code runtime} overlay package declares
 * {@code ReadMemStats}; the frontend lowers the call to this additive
 * wire node (same pattern as {@code "runtimenumcpu"}). Populates
 * {@code *m} from JVM telemetry (see {@link RtMemStats#fillMemStats}).
 * A nil {@code m} raises gc's nil-dereference panic.
 *
 * <p>Like {@code clear}, this is a void builtin lowered as an expression
 * node whose value is discarded by the enclosing statement; the node
 * carries {@code INT64} and returns 0 after filling.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeReadMemStatsNode extends GoExprNode {

    @Child private GoExprNode m;

    private RuntimeReadMemStatsNode(GoExprNode m) {
        super(GoKind.INT64);
        this.m = m;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object v = m.executeObject(frame);
        GoStruct ms = null;
        if (v instanceof GoPointer p && !p.isNil()) {
            Object r = p.referent();
            if (r instanceof GoStruct gs) {
                ms = gs;
            }
        }
        if (ms == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            throw new AssertionError("unreachable: Panic.throwPanic returned");
        }
        RtMemStats.fillMemStats(ms);
        return 0;
    }

    /**
     * Builds the node from the {@code "runtimereadmemstats"} wire node.
     * Fields: {@code m} (the {@code *runtime.MemStats} expression node).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimereadmemstats");
        String path = "runtimereadmemstats.fields";
        Json.Value mv = f.opt("m");
        if (!(mv instanceof Json.Object mObj)) {
            throw new FrontendException(
                    path + ": runtimereadmemstats needs an \"m\" node object");
        }
        return new RuntimeReadMemStatsNode(factory.buildExpr(mObj, ctx));
    }
}
