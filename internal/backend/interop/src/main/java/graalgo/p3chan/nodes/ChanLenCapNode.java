package graalgo.p3chan.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.ChanOps;
import graalgo.values.GoKind;

/**
 * {@code len(ch)} / {@code cap(ch)} (P3-M5, WS E, ADR-0005 Appendix C):
 * the {@code "chanlen"} / {@code "chancap"} wire nodes (expression kinds).
 *
 * <p>The exporter emits these for {@code len}/{@code cap} applied to a
 * channel, because the frozen {@code LenNode} rejects {@code CHAN}
 * (WS G) — additive wire kinds only, no Phase-1/Phase-2 change.
 *
 * <p>Semantics via {@link ChanOps#len} / {@link ChanOps#cap}, which call
 * {@code GoChan.buffered()} / {@code GoChan.capacity()}; {@code TimerChan}
 * overrides both to 0 (DIV-M3-1), so timer/ticker channels observe as
 * unbuffered in every state — gc parity, including a physically buffered
 * fire value (verified in {@code tc07_timer_chan_len_cap.go}).
 *
 * <p>Nil channel: gc reports {@code len}/{@code cap} of a nil channel as
 * 0 (it is not a blocking operation), unlike {@code recv}. The
 * channel subexpression may also be produced by the frozen exporter
 * paths, so the value flows through {@link ChanWire#asChan} like the
 * other channel nodes.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1/Phase-2 change.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
public final class ChanLenCapNode extends GoExprNode {

    @Child
    private GoExprNode chanNode;

    private final boolean capMode;

    private ChanLenCapNode(GoKind intKind, GoExprNode chanNode, boolean capMode) {
        super(intKind);
        this.chanNode = chanNode;
        this.capMode = capMode;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object ch = chanNode.executeObject(frame);
        if (ch == null) {
            // gc parity: len/cap of a nil channel are 0 (no blocking).
            return Long.valueOf(0L);
        }
        String name = capMode ? "chancap" : "chanlen";
        int n = capMode ? ChanOps.cap(ChanWire.asChan(ch, name))
                        : ChanOps.len(ChanWire.asChan(ch, name));
        return Long.valueOf(n);
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
     * Builds the node from the {@code "chanlen"} / {@code "chancap"}
     * wire node. Fields: {@code x} (channel expression), {@code type}
     * (the int result's type-table id — determines this node's
     * {@link GoKind}).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory, boolean capMode) {
        String kind = capMode ? "chancap" : "chanlen";
        Json.Object f = node.object("fields", kind);
        String path = kind + ".fields";
        GoExprNode chanNode = ChanWire.exprField(f, "x", kind, factory, ctx);
        int typeId = f.intValue("type", path);
        GoKind intKind = ChanWire.kindOf(ctx, typeId, path);
        return new ChanLenCapNode(intKind, chanNode, capMode);
    }
}
