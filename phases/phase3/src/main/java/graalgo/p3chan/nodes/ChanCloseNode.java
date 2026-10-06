package graalgo.p3chan.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoStmtNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.ChanOps;
import graalgo.runtime.Panic;
import graalgo.values.GoString;

/**
 * {@code close(ch)} (P3-M5, WS E, ADR-0005 Appendix C): the
 * {@code "chanclose"} wire node (statement kind).
 *
 * <p>Semantics via {@link ChanOps#close}: close of a closed channel
 * panics ({@code "close of closed channel"} — verified against
 * {@code GoChan.close}, gc parity). Close of a nil channel panics
 * ({@code "close of nil channel"}, gc parity): {@code ChanOps.close}
 * would throw a non-Go {@code IllegalArgumentException} on null, so the
 * nil case panics here, in phase-3 code — no phase-2 change.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerStmtKind} hook — no Phase-1/Phase-2 change.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
public final class ChanCloseNode extends GoStmtNode {

    @Child
    private GoExprNode chanNode;

    private ChanCloseNode(GoExprNode chanNode) {
        this.chanNode = chanNode;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object ch = chanNode.executeObject(frame);
        if (ch == null) {
            Panic.throwPanic(GoString.fromUtf8("close of nil channel"));
        }
        ChanOps.close(ChanWire.asChan(ch, "close"));
    }

    /**
     * Builds the node from the {@code "chanclose"} wire node. Fields:
     * {@code x} (channel expression).
     */
    public static GoStmtNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "chanclose");
        GoExprNode chanNode = ChanWire.exprField(f, "x", "chanclose", factory, ctx);
        return new ChanCloseNode(chanNode);
    }
}
