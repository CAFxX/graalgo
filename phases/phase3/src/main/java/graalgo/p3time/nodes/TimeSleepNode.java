package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.Timers;
import graalgo.values.GoKind;

/**
 * {@code time.Sleep(d)} (P3-M5, WS E, ADR-0005 D1): the
 * {@code "timesleep"} wire node (void).
 *
 * <p>Uses the Phase-2 sleep path ({@link Timers#sleep}): the goroutine
 * parks scheduler-aware on the timer heap (GG-SCHED-0004) — it never pins
 * a carrier thread and does not count against the in-flight cap while
 * parked. {@code d <= 0} returns immediately (gc parity, inside
 * {@code Timers.sleep}).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeSleepNode extends GoExprNode {

    @Child
    private GoExprNode duration;

    private TimeSleepNode(GoExprNode duration) {
        // Void, like SyncOpNode's ops: INTERFACE kind returning null.
        super(GoKind.INTERFACE);
        this.duration = duration;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Timers.sleep(duration.executeLong(frame));
        return null;
    }

    /**
     * Builds the node from the {@code "timesleep"} wire node. Fields:
     * {@code duration} (nanos, literal or expression). No result type
     * (void).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "timesleep");
        GoExprNode duration = TimeWire.durationNode(f, "timesleep", factory, ctx);
        return new TimeSleepNode(duration);
    }
}
