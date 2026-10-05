package graalgo.sched.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.SourceSection;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoStmtNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.runtime.Scheduler;
import graalgo.values.GoFunc;

/**
 * The {@code go} statement (P2-M1, WS A).
 *
 * <p>Evaluates the function value in the <em>spawning</em> goroutine (Go
 * semantics: the function and arguments are evaluated before the new
 * goroutine starts), then hands the {@link GoFunc} to the frozen
 * {@code Scheduler.spawn(GoFunc)}. Argument binding arrives with the
 * call protocol in P2-M2+; P2-M1 supports zero-arg spawns only and
 * fails fast otherwise.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerStmtKind} extension hook — no Phase-1
 * {@code NodeFactory} change needed. The exporter emits the
 * {@code "gostmt"} wire node (additive v1 kind).
 */
public final class GoStatementNode extends GoStmtNode {
    @Child
    private GoExprNode fn;

    public GoStatementNode(GoExprNode fn) {
        this.fn = fn;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object f = fn.executeObject(frame);
        if (f == null) {
            Panic.throwPanic(GoErrors.nilDereference());
        }
        if (!(f instanceof GoFunc gf)) {
            throw new FrontendException("go statement requires a function value, got " + f);
        }
        Scheduler.spawn(gf);
    }

    /**
     * Builds the node from the {@code "gostmt"} wire node. The wire
     * shape reuses the exported call: {@code fields.call} is a
     * {@code "call"} node whose {@code fun} is the function expression.
     */
    public static GoStmtNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "gostmt");
        String path = "gostmt.fields";
        Json.Object call = f.object("call", path);
        Json.Object cf = call.object("fields", path + ".call");
        String cpath = path + ".call.fields";
        GoExprNode fn = factory.buildExpr((Json.Object) cf.object("fun", cpath), ctx);
        Json.Array args = cf.array("args", cpath);
        if (args.size() > 0) {
            throw new FrontendException(
                    "go with arguments is not supported in P2-M1 (call protocol: P2-M2+)");
        }
        GoStatementNode n = new GoStatementNode(fn);
        Json.Object pos = node.object("pos", "gostmt");
        String p = "gostmt.pos";
        SourceSection ss = ctx.source.createSection(
                pos.intValue("line", p), pos.intValue("col", p), pos.intValue("len", p));
        n.setSourceSection(ss);
        return n;
    }
}
