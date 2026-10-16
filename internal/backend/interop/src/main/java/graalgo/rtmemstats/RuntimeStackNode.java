package graalgo.rtmemstats;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * {@code runtime.Stack(buf, all)}: the {@code "runtimestack"} wire node.
 *
 * <p>The synthetic {@code runtime} overlay package declares
 * {@code Stack}; the frontend lowers the call to this additive wire node
 * (same pattern as {@code "runtimenumcpu"}). Formats the current
 * goroutine's Go stack in gc's {@code runtime.Stack} rendering into
 * {@code buf} and returns the bytes written (truncated to
 * {@code len(buf)} when the buffer is too small — gc semantics). A nil
 * {@code buf} writes nothing and returns 0.
 *
 * <p>Cross-goroutine capture is not available on this backend (the
 * scheduler exposes no per-goroutine Go stacks), so {@code all=true}
 * currently renders the calling goroutine only — a documented deviation
 * from gc, which dumps every goroutine.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeStackNode extends GoExprNode {

    @Child private GoExprNode buf;
    @Child private GoExprNode all;

    private RuntimeStackNode(GoKind kind, GoExprNode buf, GoExprNode all) {
        super(kind);
        this.buf = buf;
        this.all = all;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // Evaluate `all` for side-effect ordering. Cross-goroutine Go-stack
        // capture is unavailable on this backend (see class javadoc), so
        // both cases render the calling goroutine.
        all.executeBoolean(frame);
        byte[] text = RtMemStats.formatCurrentStack(getSourceSection());
        Object bufVal = buf.executeObject(frame);
        if (!(bufVal instanceof GoSlice slice)) {
            return 0; // nil slice: gc writes nothing, returns 0
        }
        Object arr = slice.array();
        if (!(arr instanceof byte[] bytes)) {
            throw new FrontendException("runtimestack: buf backing is not byte[]");
        }
        int n = Math.min(text.length, slice.len());
        System.arraycopy(text, 0, bytes, slice.offset(), n);
        return n;
    }

    /**
     * Builds the node from the {@code "runtimestack"} wire node. Fields:
     * {@code args} (two expression nodes: {@code buf}, {@code all}),
     * {@code type} (result type-table id — always an int kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimestack");
        String path = "runtimestack.fields";
        Json.Array arr = f.array("args", path);
        if (arr.size() != 2) {
            throw new FrontendException(path + ": runtimestack needs 2 args");
        }
        GoExprNode[] argNodes = new GoExprNode[2];
        for (int i = 0; i < 2; i++) {
            Json.Value v = arr.get(i);
            if (!(v instanceof Json.Object o)) {
                throw new FrontendException(
                        path + ".args[" + i + "]: must be a node object");
            }
            argNodes[i] = factory.buildExpr(o, ctx);
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": runtimestack needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind kind = NodeFactory.kindOf(ctx, typeId, path);
        if (!kind.isIntKind()) {
            throw new FrontendException(
                    path + ": runtimestack of non-int kind " + kind);
        }
        return new RuntimeStackNode(kind, argNodes[0], argNodes[1]);
    }
}
