package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3time.GoTimerT;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;

/**
 * The {@code .C} selector on {@code *time.Timer} / {@code *time.Ticker}
 * (P3-M5, WS E, ADR-0005 D3): the {@code "timec"} wire node.
 *
 * <p>Returns the timer's stable {@link graalgo.p3time.TimeChan}
 * ({@code time.Time} values, {@code TimerChan} flavor — DIV-M3-1:
 * {@code len}/{@code cap} report 0). A null receiver is a Go
 * nil-pointer-dereference panic (the {@code syncop} precedent).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeCNode extends GoExprNode {

    @Child
    private GoExprNode recv;

    private TimeCNode(GoExprNode recv) {
        super(GoKind.CHAN);
        this.recv = recv;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object r = recv.executeObject(frame);
        if (r == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        if (!(r instanceof GoTimerT t)) {
            throw new IllegalStateException("timec of non-timer value: " + r);
        }
        return t.userChannel();
    }

    /**
     * Builds the node from the {@code "timec"} wire node. Fields:
     * {@code recv}, {@code type} (type-table id — a channel).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "timec");
        String path = "timec.fields";
        int typeId = TimeWire.requireTypeId(f, "timec");
        TimeWire.expectKind(ctx, typeId, "timec", "chan", "result");
        GoExprNode recv = factory.buildExpr(f.object("recv", path), ctx);
        return new TimeCNode(recv);
    }
}
