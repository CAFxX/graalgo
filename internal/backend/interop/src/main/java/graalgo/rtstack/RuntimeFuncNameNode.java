package graalgo.rtstack;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code (*runtime.Func).Name()}: the {@code "runtimefuncname"} wire node.
 *
 * <p>Returns the qualified function name ({@code main} is reported as
 * {@code main.main}, like the panic traceback; other names are the
 * unqualified wire names — the wire carries no package path). A nil
 * {@code *Func} raises gc's nil-dereference panic.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeFuncNameNode extends GoExprNode {

    @Child private GoExprNode f;

    private RuntimeFuncNameNode(GoExprNode f) {
        super(GoKind.STRING);
        this.f = f;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return RtStack.goString(RtStack.derefFunc(f.executeObject(frame), this).funcName());
    }

    /**
     * Builds the node from the {@code "runtimefuncname"} wire node.
     * Fields: {@code f} (the {@code *runtime.Func} expression node),
     * {@code type} (result type-table id — a string kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimefuncname");
        String path = "runtimefuncname.fields";
        Json.Value fv = f.opt("f");
        if (!(fv instanceof Json.Object fObj)) {
            throw new FrontendException(path + ": runtimefuncname needs an \"f\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimefuncname needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.STRING) {
            throw new FrontendException(
                    path + ": runtimefuncname result must be a string kind");
        }
        return new RuntimeFuncNameNode(factory.buildExpr(fObj, ctx));
    }
}
