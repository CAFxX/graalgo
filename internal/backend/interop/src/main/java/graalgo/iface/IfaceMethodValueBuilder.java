package graalgo.iface;

import java.util.List;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * Builds {@link IfaceMethodValueNode} from the
 * {@code "ifacemethodvalue"} wire node (P1-9, review-2026-10-31-22).
 *
 * <p>Wire fields: {@code recv} (evaluates to a {@code GoInterface}),
 * {@code method} (name), {@code iface} (interface type-table id — the
 * method list source, as for {@code "ifacemethod"}), {@code sig}
 * (method signature type-table id, no receiver — carried for wire
 * parity with {@code "ifacemethod"} and future argument adaptation;
 * dispatch itself resolves the target from the itab and needs no
 * signature), {@code type} (the method value's func type id).
 *
 * <p>Argument adaptation note: a direct {@code "ifacemethod"} call
 * boxes concrete arguments for interface-typed callee parameters with
 * their exact static-type descriptors at build time (see
 * {@link IfaceDispatchBuilder}). A method value's arguments are not
 * known at build time, so calls through the value adapt interface
 * parameters the way ordinary function-value calls do — via the
 * callee root's inferred wrapping (see
 * {@link graalgo.frontend.GoFuncRootNode}). That is the same behavior
 * as calling any other function value with an interface parameter, and
 * strictly weaker than a direct interface call only for callees that
 * type-assert on an interface-typed parameter's dynamic method set.
 * All other parameter kinds are unaffected.
 *
 * <p>Registered via the public {@code NodeFactory.registerExprKind}
 * hook — no Phase-1 {@code NodeFactory} change.
 */
public final class IfaceMethodValueBuilder implements NodeFactory.ExprBuilder {
    private final NodeFactory factory;

    public IfaceMethodValueBuilder(NodeFactory factory) {
        this.factory = factory;
    }

    @Override
    public GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "ifacemethodvalue");
        String path = "ifacemethodvalue.fields";
        GoExprNode recv = factory.buildExpr(f.object("recv", path), ctx);
        String method = f.string("method", path);
        int ifaceId = f.intValue("iface", path);
        // Validated for wire shape; dispatch needs no signature (see
        // the class javadoc). intValue throws on a missing/wrong field.
        f.intValue("sig", path);
        f.intValue("type", path);
        if (recv.kind() != GoKind.INTERFACE) {
            throw new FrontendException(path + ": ifacemethodvalue \"recv\" kind is "
                    + recv.kind() + ", want INTERFACE (the frontend rejects "
                    + "non-interface method values before this node)");
        }
        List<String> ifaceMethodNames = IfaceDispatchBuilder.ifaceMethodNames(ctx, ifaceId, path);
        IfaceDispatcher dispatcher = new IfaceDispatcher(method, ifaceMethodNames, ifaceId);
        IfaceMethodValueRootNode root = new IfaceMethodValueRootNode(dispatcher, ctx.funcObjId);
        return new IfaceMethodValueNode(recv, root.getCallTarget());
    }
}
