package graalgo.p3abi;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.Panic;
import graalgo.values.FuncIds;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * The {@code internal/abi} {@code FuncPCABI0}/{@code FuncPCABIInternal}
 * compile intrinsics (real-reflect Phase 1): the {@code "abifuncpc"} wire
 * node.
 *
 * <p>gc implements these as compile intrinsics returning the entry PC of a
 * func value. graalgo has no PCs, so the node returns an opaque stable func
 * id from the {@link FuncIds} registry: same function → same id, non-zero
 * for non-nil funcs. Fields: {@code "kind"} (the intrinsic name, for error
 * messages), {@code "arg"} (the func value, as {@code any}), {@code "type"}
 * (result {@code uintptr} type-table id).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class AbiFuncPCNode extends GoExprNode {
    private final String kind;
    @Child private GoExprNode arg;

    private AbiFuncPCNode(String kind, GoExprNode arg) {
        super(GoKind.UINT64);
        this.kind = kind;
        this.arg = arg;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object v = arg.executeObject(frame);
        if (v instanceof GoInterface gi) {
            if (gi.isNil()) {
                return 0L;
            }
            v = gi.data();
        }
        if (v == null) {
            return 0L;
        }
        if (v instanceof GoFunc fn) {
            return FuncIds.idFor(fn);
        }
        Panic.throwPanic(GoString.fromUtf8("abi." + kind + ": argument is not a func value"));
        throw new AssertionError("unreachable: Panic.throwPanic returned");
    }

    /**
     * Builds the node from the {@code "abifuncpc"} wire node. Fields:
     * {@code kind} (string), {@code arg} (expr node), {@code type} (result
     * type-table id — must be {@code uintptr}).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
            NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "abifuncpc");
        String path = "abifuncpc.fields";
        String kind = f.string("kind", path);
        if (!"FuncPCABI0".equals(kind) && !"FuncPCABIInternal".equals(kind)) {
            throw new FrontendException(path + ": unknown abifuncpc kind " + kind);
        }
        Json.Value av = f.opt("arg");
        if (!(av instanceof Json.Object ao)) {
            throw new FrontendException(path + ": abifuncpc needs \"arg\"");
        }
        GoExprNode argNode = factory.buildExpr(ao, ctx);
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": abifuncpc needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind resultKind = NodeFactory.kindOf(ctx, typeId, path);
        if (resultKind != GoKind.UINT64) {
            throw new FrontendException(
                    path + ": abifuncpc result must be uintptr, got " + resultKind);
        }
        GoExprNode n = new AbiFuncPCNode(kind, argNode);
        n.setTypeId(typeId);
        return n;
    }
}
