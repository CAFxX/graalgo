package graalgo.rtstack;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code (*runtime.Func).Entry()}: the {@code "runtimefuncentry"} wire
 * node.
 *
 * <p>Returns the interned entry PC for the function (its definition
 * site), or 0 when unknown — gc: "Entry ... may be zero if not known". A
 * nil {@code *Func} raises gc's nil-dereference panic.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeFuncEntryNode extends GoExprNode {

    @Child private GoExprNode f;

    private RuntimeFuncEntryNode(GoKind kind, GoExprNode f) {
        super(kind);
        this.f = f;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return RtStack.derefFunc(f.executeObject(frame), this).entry();
    }

    /**
     * Builds the node from the {@code "runtimefuncentry"} wire node.
     * Fields: {@code f} (the {@code *runtime.Func} expression node),
     * {@code type} (result type-table id — a uintptr/int kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimefuncentry");
        String path = "runtimefuncentry.fields";
        Json.Value fv = f.opt("f");
        if (!(fv instanceof Json.Object fObj)) {
            throw new FrontendException(path + ": runtimefuncentry needs an \"f\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimefuncentry needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind kind = NodeFactory.kindOf(ctx, typeId, path);
        if (!kind.isIntKind()) {
            throw new FrontendException(
                    path + ": runtimefuncentry of non-int kind " + kind);
        }
        return new RuntimeFuncEntryNode(kind, factory.buildExpr(fObj, ctx));
    }
}
