package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3time.GoTimerT;
import graalgo.p3time.TimeDescriptors;
import graalgo.runtime.GoTimer;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * {@code time.After(d)} (P3-M5, WS E, ADR-0005 D1): the
 * {@code "timeafter"} wire node, evaluating to a {@code <-chan Time}.
 *
 * <p>The channel is the {@link graalgo.p3time.TimeChan} of an internal
 * one-shot {@link GoTimerT} ({@code TimerChan} flavor — DIV-M3-1:
 * {@code len}/{@code cap} report 0). The opaque timer itself is not
 * exposed to Go code; its translator goroutine keeps it alive until the
 * fire value is delivered.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeAfterNode extends GoExprNode {

    @Child
    private GoExprNode duration;
    private final GoStructDescriptor timeDesc;

    private TimeAfterNode(GoExprNode duration, GoStructDescriptor timeDesc) {
        super(GoKind.CHAN);
        this.duration = duration;
        this.timeDesc = timeDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long d = duration.executeLong(frame);
        GoTimerT t = new GoTimerT(GoTimer.newTimer(d), false, null, timeDesc);
        return t.userChannel();
    }

    /**
     * Builds the node from the {@code "timeafter"} wire node. Fields:
     * {@code duration} (nanos, literal or expression), {@code type}
     * (type-table id — a channel).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "timeafter");
        String path = "timeafter.fields";
        int typeId = TimeWire.requireTypeId(f, "timeafter");
        TimeWire.expectKind(ctx, typeId, "timeafter", "chan", "result");
        GoExprNode duration = TimeWire.durationNode(f, "timeafter", factory, ctx);
        GoStructDescriptor timeDesc = TimeDescriptors.resolve(ctx, path);
        return new TimeAfterNode(duration, timeDesc);
    }
}
