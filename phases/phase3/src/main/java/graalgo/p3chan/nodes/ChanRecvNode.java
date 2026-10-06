package graalgo.p3chan.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.jfr.ChannelOp;
import graalgo.runtime.ChanOps;
import graalgo.values.GoKind;

/**
 * {@code <-ch} (P3-M5, WS E, ADR-0005 Appendix C): the {@code "recv"}
 * wire node (expression kind).
 *
 * <p>Semantics via {@link ChanOps#recv} (scheduler-aware park; recv on a
 * closed, drained channel yields the element zero value — which is why
 * this node carries {@code type}, the element type-table id: the node's
 * {@link GoKind} is the element kind, resolved here and never guessed).
 * Recv on a nil channel blocks forever (gc parity): parked here, in
 * phase-3 code — no phase-2 change.
 *
 * <p>The two-value form {@code v, ok := <-ch} is <em>not</em> this node:
 * the exporter lowers it to a single-case {@code "select"} (which parks
 * exactly like a blocking receive) so the ok bit flows through the
 * select's {@code targets} — see {@link SelectNode}. No extra wire kind.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1/Phase-2 change.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
public final class ChanRecvNode extends GoExprNode {

    @Child
    private GoExprNode chanNode;

    private ChanRecvNode(GoKind elemKind, GoExprNode chanNode) {
        super(elemKind);
        this.chanNode = chanNode;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object ch = chanNode.executeObject(frame);
        if (ch == null) {
            ChanWire.parkForeverNilChannel(ChannelOp.RECV);
        }
        return ChanOps.recv(ChanWire.asChan(ch, "recv"));
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return ((Number) executeObject(frame)).longValue();
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) executeObject(frame)).intValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Number) executeObject(frame)).doubleValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Number) executeObject(frame)).floatValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return (Boolean) executeObject(frame);
    }

    /**
     * Builds the node from the {@code "recv"} wire node. Fields:
     * {@code x} (channel expression), {@code type} (element type-table
     * id — determines this node's {@link GoKind}).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "recv");
        String path = "recv.fields";
        GoExprNode chanNode = ChanWire.exprField(f, "x", "recv", factory, ctx);
        int typeId = f.intValue("type", path);
        GoKind elemKind = ChanWire.kindOf(ctx, typeId, path);
        return new ChanRecvNode(elemKind, chanNode);
    }
}
