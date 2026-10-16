package graalgo.rtmemstats;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code runtime.Version()}: the {@code "runtimeversion"} wire node.
 *
 * <p>The synthetic {@code runtime} overlay package declares
 * {@code Version}; the frontend lowers the call to this additive wire
 * node (same pattern as {@code "runtimenumcpu"}). Returns the Go release
 * tag of the toolchain this backend emulates (gc: the link-time
 * {@code buildVersion}, e.g. {@code "go1.27.1"}).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeVersionNode extends GoExprNode {

    private RuntimeVersionNode() {
        super(GoKind.STRING);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return RtMemStats.goString(GoToolchain.version());
    }

    /**
     * Builds the node from the {@code "runtimeversion"} wire node. Fields:
     * {@code type} (result type-table id — always a string kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "runtimeversion");
        String path = "runtimeversion.fields";
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimeversion needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.STRING) {
            throw new FrontendException(
                    path + ": runtimeversion result must be a string kind");
        }
        return new RuntimeVersionNode();
    }
}
