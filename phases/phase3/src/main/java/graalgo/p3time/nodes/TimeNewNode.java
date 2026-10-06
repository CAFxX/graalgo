package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3time.GoTickerT;
import graalgo.p3time.GoTimerT;
import graalgo.p3time.TimeDescriptors;
import graalgo.runtime.GoTimer;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * Construction of a {@code time.Timer} / {@code time.Ticker} (P3-M5, WS E,
 * ADR-0005 D3): the {@code "timenew"} wire node.
 *
 * <p>The Go value <em>is</em> the opaque {@link GoTimerT} /
 * {@link GoTickerT} Java object (the ADR-0002 {@code sync.Mutex}
 * pattern). Non-positive durations: {@code NewTicker} panics with gc's
 * message (via {@link GoTimer#newTicker}); {@code NewTimer} fires
 * immediately (via {@link GoTimer#newTimer}).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeNewNode extends GoExprNode {

    private final boolean ticker;
    @Child
    private GoExprNode duration;
    private final GoStructDescriptor timeDesc;

    private TimeNewNode(boolean ticker, GoExprNode duration, GoStructDescriptor timeDesc) {
        super(GoKind.POINTER);
        this.ticker = ticker;
        this.duration = duration;
        this.timeDesc = timeDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long d = duration.executeLong(frame);
        GoTimer t = ticker ? GoTimer.newTicker(d) : GoTimer.newTimer(d);
        return ticker ? new GoTickerT(t, timeDesc) : new GoTimerT(t, false, null, timeDesc);
    }

    /**
     * Builds the node from the {@code "timenew"} wire node. Fields:
     * {@code kind} ("timer"|"ticker"), {@code duration} (nanos, literal or
     * expression), {@code type} (type-table id — a pointer, the
     * {@code *time.Timer} / {@code *time.Ticker} result).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "timenew");
        String path = "timenew.fields";
        boolean ticker = switch (f.string("kind", path)) {
            case "timer" -> false;
            case "ticker" -> true;
            default -> throw new FrontendException(
                    path + ": unknown timenew kind " + f.string("kind", path));
        };
        int typeId = TimeWire.requireTypeId(f, "timenew");
        TimeWire.expectKind(ctx, typeId, "timenew", "pointer", "result");
        GoExprNode duration = TimeWire.durationNode(f, "timenew", factory, ctx);
        GoStructDescriptor timeDesc = TimeDescriptors.resolve(ctx, path);
        return new TimeNewNode(ticker, duration, timeDesc);
    }
}
