package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3time.GoTimerT;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;

/**
 * {@code (*Timer).Stop} / {@code (*Ticker).Stop} /
 * {@code (*Timer).Reset} / {@code (*Ticker).Reset} (P3-M5, WS E,
 * ADR-0005 D3): the {@code "timeop"} wire node, evaluating to bool.
 *
 * <p>gc semantics come from {@link GoTimerT#stop()} /
 * {@link GoTimerT#reset(long)} (Phase-2 heap truth): Stop reports whether
 * the timer had not yet fired (fired-undrained counts as active);
 * {@code Ticker.Reset} panics on a non-positive interval with gc's
 * message. A null receiver is a Go nil-pointer-dereference panic (the
 * {@code syncop} precedent).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeOpNode extends GoExprNode {

    private final String op;
    @Child
    private GoExprNode recv;
    @Child
    private GoExprNode duration; // null unless op == "Reset"

    private TimeOpNode(String op, GoExprNode recv, GoExprNode duration) {
        super(GoKind.BOOL);
        this.op = op;
        this.recv = recv;
        this.duration = duration;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        Object r = recv.executeObject(frame);
        if (r == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        if (!(r instanceof GoTimerT t)) {
            throw new IllegalStateException("timeop of non-timer value: " + r);
        }
        return switch (op) {
            case "Stop" -> t.stop();
            case "Reset" -> t.reset(duration.executeLong(frame));
            default -> throw new FrontendException("unknown time op " + op);
        };
    }

    /**
     * Builds the node from the {@code "timeop"} wire node. Fields:
     * {@code op} ("Stop"|"Reset"), {@code recv}, {@code duration} (Reset
     * only, nanos, literal or expression), {@code type} (type-table id —
     * bool).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "timeop");
        String path = "timeop.fields";
        String op = f.string("op", path);
        GoExprNode duration = null;
        switch (op) {
            case "Stop" -> { /* no duration */ }
            case "Reset" -> duration = TimeWire.durationNode(f, "timeop", factory, ctx);
            default -> throw new FrontendException(path + ": unknown time op " + op);
        }
        int typeId = TimeWire.requireTypeId(f, "timeop");
        expectBool(ctx, typeId, path);
        GoExprNode recv = factory.buildExpr(f.object("recv", path), ctx);
        return new TimeOpNode(op, recv, duration);
    }

    private static void expectBool(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"basic".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": timeop result must be bool");
        }
        Json.Object detail = entry.object("detail", path);
        GoKind k;
        try {
            k = GoKind.fromBasic(detail.string("name", path), detail.string("class", path));
        } catch (IllegalArgumentException e) {
            throw new FrontendException(path + ": " + e.getMessage());
        }
        if (k != GoKind.BOOL) {
            throw new FrontendException(path + ": timeop result must be bool, got " + k);
        }
    }
}
