package graalgo.p3builtin.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code runtime.NumCPU()}: the {@code "runtimenumcpu"} wire node.
 *
 * <p>The synthetic {@code runtime} overlay package declares {@code NumCPU};
 * the frontend lowers the call to this additive wire node (same pattern as
 * {@code "minmax"}). No Phase-1 {@code NodeFactory} change.
 *
 * <p>Returns the host's available processor count, matching gc's
 * {@code runtime.NumCPU} semantics on the execution host.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class NumCPUNode extends GoExprNode {

    private NumCPUNode(GoKind kind) {
        super(kind);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return Runtime.getRuntime().availableProcessors();
    }

    /**
     * Builds the node from the {@code "runtimenumcpu"} wire node. Fields:
     * {@code type} (result type-table id — always an int kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "runtimenumcpu");
        String path = "runtimenumcpu.fields";
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimenumcpu needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind kind = NodeFactory.kindOf(ctx, typeId, path);
        if (!kind.isIntKind()) {
            throw new FrontendException(
                    path + ": runtimenumcpu of non-int kind " + kind);
        }
        return new NumCPUNode(kind);
    }
}
