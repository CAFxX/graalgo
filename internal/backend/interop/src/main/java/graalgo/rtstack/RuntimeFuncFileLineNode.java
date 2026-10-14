package graalgo.rtstack;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.StackTrace;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * {@code (*runtime.Func).FileLine(pc)}: the {@code "runtimefuncfileline"}
 * wire node.
 *
 * <p>Returns the {@code funcFileLinePair{file, line}} struct for the
 * multi-result desugar (mrRuntimeStack): the file name and line number
 * recorded for the interned PC ({@code "", 0} when the PC is unknown). A
 * nil {@code *Func} raises gc's nil-dereference panic.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeFuncFileLineNode extends GoExprNode {

    @Child private GoExprNode f;
    @Child private GoExprNode pc;
    private final GoStructDescriptor pairDesc;

    private RuntimeFuncFileLineNode(GoExprNode f, GoExprNode pc,
            GoStructDescriptor pairDesc) {
        super(GoKind.STRUCT);
        this.f = f;
        this.pc = pc;
        this.pairDesc = pairDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        RtStack.derefFunc(f.executeObject(frame), this);
        StackTrace.PcInfo info = StackTrace.lookup(pc.executeLong(frame));
        String file = info == null ? "" : info.file();
        int line = info == null ? 0 : info.line();
        long[] longs = new long[pairDesc.longsCount()];
        Object[] objs = new Object[pairDesc.objectsCount()];
        objs[RtStack.objIndex(pairDesc, "file", "runtimefuncfileline")] =
                RtStack.goString(file);
        longs[RtStack.longIndex(pairDesc, "line", "runtimefuncfileline")] = line;
        return new GoStruct(pairDesc, longs, objs);
    }

    /**
     * Builds the node from the {@code "runtimefuncfileline"} wire node.
     * Fields: {@code f} (the {@code *runtime.Func} expression node),
     * {@code pc} (expression node), {@code type} (result type-table id —
     * the named {@code funcFileLinePair} struct).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimefuncfileline");
        String path = "runtimefuncfileline.fields";
        Json.Value fv = f.opt("f");
        Json.Value pv = f.opt("pc");
        if (!(fv instanceof Json.Object fObj)) {
            throw new FrontendException(
                    path + ": runtimefuncfileline needs an \"f\" node object");
        }
        if (!(pv instanceof Json.Object pcObj)) {
            throw new FrontendException(
                    path + ": runtimefuncfileline needs a \"pc\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(
                    path + ": runtimefuncfileline needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.STRUCT) {
            throw new FrontendException(
                    path + ": runtimefuncfileline result must be a struct kind");
        }
        GoStructDescriptor desc = NodeFactory.structDescriptorFor(ctx, typeId, path);
        return new RuntimeFuncFileLineNode(factory.buildExpr(fObj, ctx),
                factory.buildExpr(pcObj, ctx), desc);
    }
}
