package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3time.GoTickerT;
import graalgo.p3time.TimeDescriptors;
import graalgo.runtime.GoTimer;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * {@code time.Tick(d)} (P3-M5, WS E, ADR-0005 D1): the
 * {@code "timetick"} wire node, evaluating to a {@code <-chan Time}.
 *
 * <p>The channel is the {@link graalgo.p3time.TimeChan} of an internal
 * ticker ({@code TimerChan} flavor — DIV-M3-1). Like gc, the ticker
 * cannot be stopped (documented leak): its translator goroutine is a
 * registered background translator, shut down at program end by the
 * launcher's two-phase quiescence. A non-positive duration yields the nil
 * channel (gc parity — unlike {@code NewTicker}, {@code Tick} does not
 * panic).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeTickNode extends GoExprNode {

    @Child
    private GoExprNode duration;
    private final GoStructDescriptor timeDesc;

    private TimeTickNode(GoExprNode duration, GoStructDescriptor timeDesc) {
        super(GoKind.CHAN);
        this.duration = duration;
        this.timeDesc = timeDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long d = duration.executeLong(frame);
        if (d <= 0) {
            return null; // gc: Tick returns nil on non-positive interval
        }
        GoTickerT t = new GoTickerT(GoTimer.newTicker(d), timeDesc);
        return t.userChannel();
    }

    /**
     * Builds the node from the {@code "timetick"} wire node. Fields:
     * {@code duration} (nanos, literal or expression), {@code type}
     * (type-table id — a channel).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "timetick");
        String path = "timetick.fields";
        int typeId = TimeWire.requireTypeId(f, "timetick");
        TimeWire.expectKind(ctx, typeId, "timetick", "chan", "result");
        GoExprNode duration = TimeWire.durationNode(f, "timetick", factory, ctx);
        GoStructDescriptor timeDesc = TimeDescriptors.resolve(ctx, path);
        return new TimeTickNode(duration, timeDesc);
    }
}
