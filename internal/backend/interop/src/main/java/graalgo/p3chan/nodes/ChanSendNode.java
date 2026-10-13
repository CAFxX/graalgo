package graalgo.p3chan.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoStmtNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.jfr.ChannelOp;
import graalgo.runtime.ChanOps;

/**
 * {@code ch <- v} (P3-M5, WS E, ADR-0005 Appendix C): the {@code "send"}
 * wire node (statement kind).
 *
 * <p>Semantics via {@link ChanOps#send} (scheduler-aware park; send on a
 * closed channel panics — gc parity, already in {@code GoChan}). Send on
 * a nil channel blocks forever (gc parity): {@code ChanOps.send} cannot
 * express that (its contract requires a {@code GoChan}), so the nil case
 * parks here, in phase-3 code — no phase-2 change.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerStmtKind} hook — no Phase-1/Phase-2 change.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
public final class ChanSendNode extends GoStmtNode {

    @Child
    private GoExprNode chanNode;
    @Child
    private GoExprNode valueNode;

    private ChanSendNode(GoExprNode chanNode, GoExprNode valueNode) {
        this.chanNode = chanNode;
        this.valueNode = valueNode;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        // gc evaluation order: channel, then value. The value is read
        // kind-aware: a primitive-kind value node (e.g. an int literal)
        // does not carry its value in executeObject.
        Object ch = chanNode.executeObject(frame);
        Object v = ChanWire.evalBoxed(valueNode, frame);
        if (ch == null) {
            ChanWire.parkForeverNilChannel(ChannelOp.SEND);
        }
        ChanOps.send(ChanWire.asChan(ch, "send"), v);
    }

    /**
     * Builds the node from the {@code "send"} wire node. Fields:
     * {@code chan} (channel expression), {@code value} (sent expression).
     */
    public static GoStmtNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "send");
        String path = "send.fields";
        GoExprNode chanNode = ChanWire.exprField(f, "chan", "send", factory, ctx);
        GoExprNode valueNode = ChanWire.exprField(f, "value", "send", factory, ctx);
        return new ChanSendNode(chanNode, valueNode);
    }
}
