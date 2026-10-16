package graalgo.rtmemstats;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code runtime.GOROOT()}: the {@code "runtimegoroot"} wire node.
 *
 * <p>The synthetic {@code runtime} overlay package declares
 * {@code GOROOT}; the frontend lowers the call to this additive wire
 * node (same pattern as {@code "runtimenumcpu"}). Returns the
 * {@code GOROOT} environment value at process start when set, else the
 * emulated toolchain's root (gc: {@code defaultGOROOT}).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeGoRootNode extends GoExprNode {

    private RuntimeGoRootNode() {
        super(GoKind.STRING);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return RtMemStats.goString(GoToolchain.goroot());
    }

    /**
     * Builds the node from the {@code "runtimegoroot"} wire node. Fields:
     * {@code type} (result type-table id — always a string kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "runtimegoroot");
        String path = "runtimegoroot.fields";
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimegoroot needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.STRING) {
            throw new FrontendException(
                    path + ": runtimegoroot result must be a string kind");
        }
        return new RuntimeGoRootNode();
    }
}
