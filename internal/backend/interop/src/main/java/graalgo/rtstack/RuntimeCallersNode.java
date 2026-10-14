package graalgo.rtstack;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.SourceSection;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.StackTrace;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * {@code runtime.Callers(skip, pc)}: the {@code "runtimecallers"} wire
 * node.
 *
 * <p>Fills {@code pc} with the interned PCs of the calling goroutine's
 * stack and returns the number of entries written. gc skip semantics: 0
 * identifies the frame for {@code Callers} itself (emitted as a synthetic
 * {@code runtime.Callers} frame attributed to the call site — there is no
 * machine frame for the intrinsic), 1 the caller of {@code Callers}, and
 * so on. Each recorded PC identifies the call site within that frame's
 * function, like gc's return PCs adjusted by {@code CallersFrames}.
 *
 * <p>PCs are opaque dense ids ({@link StackTrace}); only
 * {@code CallersFrames}/{@code FuncForPC} can interpret them — exactly
 * gc's contract.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeCallersNode extends GoExprNode {

    @Child private GoExprNode skip;
    @Child private GoExprNode pc;

    private RuntimeCallersNode(GoKind kind, GoExprNode skip, GoExprNode pc) {
        super(kind);
        this.skip = skip;
        this.pc = pc;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        int nskip = (int) skip.executeLong(frame);
        if (nskip < 0) {
            nskip = 0;
        }
        // Attribute the innermost frame to this call site before walking
        // (mirrors FuncValueCallNode for real calls).
        List<StackTrace.FrameData> frames = StackTrace.captureAt(getSourceSection());
        int total = 1 + frames.size();
        int start = Math.min(nskip, total);
        Object pcVal = pc.executeObject(frame);
        if (!(pcVal instanceof GoSlice slice)) {
            return 0; // nil slice: gc writes nothing, returns 0
        }
        Object arr = slice.array();
        if (!(arr instanceof long[] longs)) {
            throw new FrontendException(
                    "runtimecallers: []uintptr backing is not long[]");
        }
        SourceSection cs = getSourceSection();
        String csFile = "";
        int csLine = 0;
        if (cs != null) {
            if (cs.getSource() != null && cs.getSource().getName() != null) {
                csFile = cs.getSource().getName();
            }
            csLine = cs.getStartLine();
        }
        int off = slice.offset();
        int w = 0;
        for (int i = start; i < total && w < slice.len(); i++) {
            long id;
            if (i == 0) {
                id = StackTrace.intern("runtime.Callers", csFile, csLine);
            } else {
                StackTrace.FrameData f = frames.get(i - 1);
                id = StackTrace.intern(f.funcName(), f.file(), f.line());
            }
            longs[off + w] = id;
            w++;
        }
        return w;
    }

    /**
     * Builds the node from the {@code "runtimecallers"} wire node. Fields:
     * {@code skip}, {@code pc} (expression nodes), {@code type} (result
     * type-table id — always an int kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimecallers");
        String path = "runtimecallers.fields";
        Json.Value sv = f.opt("skip");
        Json.Value pv = f.opt("pc");
        if (!(sv instanceof Json.Object skipObj)) {
            throw new FrontendException(path + ": runtimecallers needs a \"skip\" node object");
        }
        if (!(pv instanceof Json.Object pcObj)) {
            throw new FrontendException(path + ": runtimecallers needs a \"pc\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimecallers needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind kind = NodeFactory.kindOf(ctx, typeId, path);
        if (!kind.isIntKind()) {
            throw new FrontendException(
                    path + ": runtimecallers of non-int kind " + kind);
        }
        return new RuntimeCallersNode(kind, factory.buildExpr(skipObj, ctx),
                factory.buildExpr(pcObj, ctx));
    }
}
