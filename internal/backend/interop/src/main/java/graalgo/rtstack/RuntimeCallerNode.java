package graalgo.rtstack;

import java.util.List;

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
 * {@code runtime.Caller(skip)}: the {@code "runtimecaller"} wire node.
 *
 * <p>Returns the {@code callerPair{pc, file, line, ok}} struct for the
 * multi-result desugar (mrRuntimeStack): the call-site PC, file and line
 * {@code skip} frames out (0 identifies the caller of {@code Caller},
 * like gc), and {@code ok=false} with zero values when the stack is not
 * that deep.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeCallerNode extends GoExprNode {

    @Child private GoExprNode skip;
    private final GoStructDescriptor pairDesc;

    private RuntimeCallerNode(GoExprNode skip, GoStructDescriptor pairDesc) {
        super(GoKind.STRUCT);
        this.skip = skip;
        this.pairDesc = pairDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        int nskip = (int) skip.executeLong(frame);
        if (nskip < 0) {
            nskip = 0;
        }
        List<StackTrace.FrameData> frames = StackTrace.captureAt(getSourceSection());
        long pc = 0;
        String file = "";
        int line = 0;
        boolean ok = false;
        if (nskip < frames.size()) {
            StackTrace.FrameData f = frames.get(nskip);
            pc = StackTrace.intern(f.funcName(), f.file(), f.line());
            file = f.file();
            line = f.line();
            ok = true;
        }
        long[] longs = new long[pairDesc.longsCount()];
        Object[] objs = new Object[pairDesc.objectsCount()];
        longs[RtStack.longIndex(pairDesc, "pc", "runtimecaller")] = pc;
        objs[RtStack.objIndex(pairDesc, "file", "runtimecaller")] = RtStack.goString(file);
        longs[RtStack.longIndex(pairDesc, "line", "runtimecaller")] = line;
        longs[RtStack.longIndex(pairDesc, "ok", "runtimecaller")] = ok ? 1 : 0;
        return new GoStruct(pairDesc, longs, objs);
    }

    /**
     * Builds the node from the {@code "runtimecaller"} wire node. Fields:
     * {@code skip} (expression node), {@code type} (result type-table id
     * — the named {@code callerPair} struct).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimecaller");
        String path = "runtimecaller.fields";
        Json.Value sv = f.opt("skip");
        if (!(sv instanceof Json.Object skipObj)) {
            throw new FrontendException(path + ": runtimecaller needs a \"skip\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimecaller needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.STRUCT) {
            throw new FrontendException(path + ": runtimecaller result must be a struct kind");
        }
        GoStructDescriptor desc = NodeFactory.structDescriptorFor(ctx, typeId, path);
        return new RuntimeCallerNode(factory.buildExpr(skipObj, ctx), desc);
    }
}
