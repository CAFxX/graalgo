package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3time.GoTimerT;
import graalgo.p3time.TimeDescriptors;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoTimer;
import graalgo.runtime.Panic;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * {@code time.AfterFunc(d, f)} (P3-M5, WS E, ADR-0005 D1): the
 * {@code "timeafterfunc"} wire node, evaluating to an opaque
 * {@code *Timer} ({@link GoTimerT}).
 *
 * <p>On fire, {@code f} runs in its own goroutine, spawned through
 * {@link graalgo.runtime.Scheduler#spawn} — the exact path {@code go}
 * statements use (never on the timer thread). Stop before the fire
 * prevents f from ever running; the Stop/Reset booleans carry gc's
 * AfterFunc meaning via {@link GoTimerT}'s commit protocol.
 *
 * <p>A nil func value is a Go nil-function panic (the {@code syncop}
 * precedent for {@code Once.Do}).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeAfterFuncNode extends GoExprNode {

    @Child
    private GoExprNode duration;
    @Child
    private GoExprNode func;
    private final GoStructDescriptor timeDesc;

    private TimeAfterFuncNode(GoExprNode duration, GoExprNode func,
                             GoStructDescriptor timeDesc) {
        super(GoKind.POINTER);
        this.duration = duration;
        this.func = func;
        this.timeDesc = timeDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object fv = func.executeObject(frame);
        if (!(fv instanceof GoFunc)) {
            // fv is null only: a nil func value is a Go nil-dereference
            // panic (GoCalls.nilFuncCall parity, cf. SyncOpNode).
            // throwPanic always throws; the AssertionError is for the compiler.
            Panic.throwPanic(GoCalls.nilFuncCall(), null, null, this);
            throw new AssertionError("unreachable");
        }
        long d = duration.executeLong(frame);
        return new GoTimerT(GoTimer.newTimer(d), false, (GoFunc) fv, timeDesc);
    }

    /**
     * Builds the node from the {@code "timeafterfunc"} wire node. Fields:
     * {@code duration} (nanos, literal or expression), {@code func} (the
     * function value), {@code type} (type-table id — a pointer, the
     * {@code *time.Timer} result).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "timeafterfunc");
        String path = "timeafterfunc.fields";
        int typeId = TimeWire.requireTypeId(f, "timeafterfunc");
        TimeWire.expectKind(ctx, typeId, "timeafterfunc", "pointer", "result");
        GoExprNode duration = TimeWire.durationNode(f, "timeafterfunc", factory, ctx);
        GoExprNode func = factory.buildExpr(f.object("func", path), ctx);
        GoStructDescriptor timeDesc = TimeDescriptors.resolve(ctx, path);
        return new TimeAfterFuncNode(duration, func, timeDesc);
    }
}
