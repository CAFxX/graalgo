package graalgo.rtstack;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoFuncInfo;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * {@code runtime.FuncForPC(pc)}: the {@code "runtimefuncforpc"} wire node.
 *
 * <p>Returns a {@code *runtime.Func} describing the function containing
 * the PC, or Go nil when the PC is unknown. (Java null is the
 * nil {@code *Func}; a non-nil result is a {@link GoPointer} to a
 * {@link GoFuncInfo}.) The PC may come from either func-PC id space:
 * {@code runtime.Callers}-interned PCs or the {@code internal/abi}
 * entry ids that {@code reflect.Value.Pointer()} returns for funcs
 * (see {@link GoFuncInfo#forPc(long)}).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeFuncForPCNode extends GoExprNode {

    @Child private GoExprNode pc;

    private RuntimeFuncForPCNode(GoExprNode pc) {
        super(GoKind.POINTER);
        this.pc = pc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoFuncInfo info = GoFuncInfo.forPc(pc.executeLong(frame));
        return info == null ? null : new GoPointer(info);
    }

    /**
     * Builds the node from the {@code "runtimefuncforpc"} wire node.
     * Fields: {@code pc} (expression node), {@code type} (result
     * type-table id — {@code *runtime.Func}, a pointer kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimefuncforpc");
        String path = "runtimefuncforpc.fields";
        Json.Value pv = f.opt("pc");
        if (!(pv instanceof Json.Object pcObj)) {
            throw new FrontendException(path + ": runtimefuncforpc needs a \"pc\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimefuncforpc needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.POINTER) {
            throw new FrontendException(
                    path + ": runtimefuncforpc result must be a pointer kind");
        }
        return new RuntimeFuncForPCNode(factory.buildExpr(pcObj, ctx));
    }
}
