package graalgo.p3sync.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoErrors;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.Panic;
import graalgo.runtime.sync.GoMutex;
import graalgo.runtime.sync.GoPool;
import graalgo.values.GoInterface;
import graalgo.values.GoString;
import java.nio.charset.StandardCharsets;
import graalgo.runtime.sync.GoOnce;
import graalgo.runtime.sync.GoPool;
import graalgo.runtime.sync.GoRWMutex;
import graalgo.runtime.sync.GoWaitGroup;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

import java.util.Map;

/**
 * A method call on a {@code sync} primitive (P3-M2b, WS D, ADR-0002):
 * the {@code "syncop"} wire node.
 *
 * <p>The receiver evaluates to the Java primitive object itself
 * (ADR-0002 D1); a null receiver is a Go nil-pointer-dereference panic.
 * Misuse panics from the primitives ({@code IllegalStateException} with
 * gc's message text) are translated to Go panics via
 * {@link Panic#throwPanic} (ADR-0002 D4):
 * <ul>
 *   <li>{@code sync: negative WaitGroup counter} (WaitGroup.Add and
 *       WaitGroup.Done) — gc panics: full parity;</li>
 *   <li>{@code sync: unlock of unlocked mutex} /
 *       {@code sync: Unlock of unlocked RWMutex} /
 *       {@code sync: RUnlock of unlocked RWMutex} — gc <em>fatal-errors</em>:
 *       same message text and exit code, but ours is a (recoverable) Go
 *       panic — scoped divergence, re-test trigger in ADR-0002 D4.</li>
 * </ul>
 *
 * <p>{@code Once.Do} invokes the func value synchronously on the winning
 * goroutine through {@link GoCalls#invoke} (the {@code FuncValueCallNode}
 * call protocol), including its {@code recoverAllowed} dance — a direct
 * {@code recover()} inside the Do func returns nil, gc parity.
 *
 * <p>P3-M11 ({@code sync.Pool}): {@code Put}/{@code Get} move
 * {@code any}-valued items through the {@link GoPool} (the Put arg is
 * boxed to an interface value by the builder, the atomic.Value
 * precedent); {@code Get} invokes the pool's {@code New} func when empty
 * and it is set. The {@code New} field itself is addressed by the
 * exporter-synthesized field ops {@code GetNew} (read), {@code SetNew}
 * (write) and {@code CallNew} ({@code p.New()} invocation) — the Go
 * value is the opaque {@link GoPool} object, so no struct-field node
 * applies. {@code Get}/{@code CallNew} return {@code any} values and
 * {@code GetNew} the func value; the rest are void and return null,
 * mirroring {@code GoexitCallNode}.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook (P2-D8 precedent) — no
 * Phase-1 {@code NodeFactory} change.
 *
 * <p>verifies: GG-0017 (sync slice)
 */
public final class SyncOpNode extends GoExprNode {

    @Child
    private GoExprNode recv;
    @Children
    private GoExprNode[] args;

    private final String op;
    private final String syncType;

    /**
     * {@code "Pool.Get"}/{@code "Pool.GetNew"}/{@code "Pool.CallNew"} →
     * result kind; void ops ({@code Put}, {@code SetNew}, and all the
     * P3-M2b mutex ops) are absent and stay {@link GoKind#INTERFACE}
     * returning null. The atomic.Value precedent ({@link AtomicOpNode})
     * for value-returning overlay ops.
     */
    private static final Map<String, GoKind> RESULT_KINDS = Map.of(
            "Pool.Get", GoKind.INTERFACE,
            "Pool.GetNew", GoKind.FUNC,
            "Pool.CallNew", GoKind.INTERFACE);

    private SyncOpNode(String op, String syncType, GoKind resultKind,
            GoExprNode recv, GoExprNode[] args) {
        super(resultKind);
        this.op = op;
        this.syncType = syncType;
        this.recv = recv;
        this.args = args;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object r = recv.executeObject(frame);
        if (r == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        switch (syncType + "." + op) {
            case "Mutex.Lock" -> ((GoMutex) r).lock();
            case "Mutex.Unlock" -> panicOnMisuse(() -> ((GoMutex) r).unlock());
            case "RWMutex.Lock" -> ((GoRWMutex) r).lock();
            case "RWMutex.Unlock" -> panicOnMisuse(() -> ((GoRWMutex) r).unlock());
            case "RWMutex.RLock" -> ((GoRWMutex) r).rLock();
            case "RWMutex.RUnlock" -> panicOnMisuse(() -> ((GoRWMutex) r).rUnlock());
            case "WaitGroup.Add" -> panicOnMisuse(() -> ((GoWaitGroup) r).add((int) args[0].executeLong(frame)));
            case "WaitGroup.Done" -> panicOnMisuse(() -> ((GoWaitGroup) r).done());
            case "WaitGroup.Wait" -> ((GoWaitGroup) r).await();
            case "Once.Do" -> doOnce((GoOnce) r, frame);
            // P3-M11 (sync.Pool): the Go value IS the GoPool object
            // (ADR-0002 D1). Put's arg is boxed to an interface value by
            // the builder (boxToInterface, the atomic.Value precedent);
            // Get returns the pooled interface value, or null for the nil
            // interface (gc: Get may return nil even with items stored).
            case "Pool.Put" -> poolOf(r).put(args[0].executeObject(frame));
            case "Pool.Get" -> {
                GoPool pool = poolOf(r);
                Object x = pool.get();
                if (x == null) {
                    Object nf = pool.getNewFunc();
                    if (nf instanceof GoFunc f) {
                        x = invokeFuncValue(f, frame);
                    }
                }
                return x;
            }
            // P3-M11: the New func field — read (GetNew), write (SetNew),
            // and direct invocation (CallNew, for p.New()).
            case "Pool.GetNew" -> {
                return poolOf(r).getNewFunc();
            }
            case "Pool.SetNew" -> poolOf(r).setNewFunc(args[0].executeObject(frame));
            case "Pool.CallNew" -> {
                return invokeNewFunc(poolOf(r).getNewFunc(), frame);
            }
            default -> throw new FrontendException("unknown sync op " + syncType + "." + op);
        }
        return null;
    }

    private static GoPool poolOf(Object r) {
        if (!(r instanceof GoPool p)) {
            throw new FrontendException("sync op on non-GoPool value: " + r.getClass().getName());
        }
        return p;
    }

    /**
     * Runs a primitive op, translating its {@code IllegalStateException}
     * misuse signal (which carries gc's message text) into a Go panic
     * (ADR-0002 D4).
     */
    private void panicOnMisuse(Runnable op) {
        try {
            op.run();
        } catch (IllegalStateException e) {
            // P3-M4: the message must be a D5 GoString, not a Java String —
            // PanicRendering renders Java Strings as "(?) 0xADDR".
            Panic.throwPanic(new GoString(e.getMessage().getBytes(StandardCharsets.UTF_8)), null, null, this);
        }
    }

    /**
     * {@code Once.Do(f)} (ADR-0002 D5): the winner runs {@code f}
     * synchronously via the {@code GoCalls} protocol; {@code GoOnce}
     * already implements exactly-once and "panic counts as done".
     */
    private void doOnce(GoOnce once, VirtualFrame frame) {
        Object fv = args[0].executeObject(frame);
        if (!(fv instanceof GoFunc f)) {
            // fv is null only: a nil func value is a Go nil-dereference
            // panic (GoCalls.nilFuncCall parity).
            Panic.throwPanic(GoCalls.nilFuncCall(), null, null, this);
            return;
        }
        once.doOnce(() -> invokeFuncValue(f, frame));
    }

    /**
     * Invokes a {@code sync.Pool}'s New func value (P3-M11, the CallNew
     * op): a nil New is a Go nil-dereference panic (calling a nil func
     * value, gc parity).
     */
    private Object invokeNewFunc(Object funcValue, VirtualFrame frame) {
        if (!(funcValue instanceof GoFunc f)) {
            Panic.throwPanic(GoCalls.nilFuncCall(), null, null, this);
            throw new AssertionError("unreachable");
        }
        return invokeFuncValue(f, frame);
    }

    /**
     * Invokes a Go func value synchronously through the {@code GoCalls}
     * protocol, including its {@code recoverAllowed} dance — a direct
     * {@code recover()} inside the func returns nil, gc parity.
     */
    private Object invokeFuncValue(GoFunc f, VirtualFrame frame) {
        // FuncValueCallNode's recoverAllowed dance: recover() is only
        // non-nil when called directly by a deferred function — clear the
        // flag for this nested call, restore afterwards.
        GoroutineState state = GoroutineState.current();
        boolean prev = state != null && state.isRecoverAllowed();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        try {
            return GoCalls.invoke(f, new Object[0]);
        } finally {
            if (state != null) {
                state.setRecoverAllowed(prev);
            }
        }
    }

    /**
     * Builds the node from the {@code "syncop"} wire node. Fields:
     * {@code op}, {@code synctyp}, {@code recv}, {@code args},
     * {@code type} (null for void ops — the factory leaves the type id
     * unset, same as other void calls).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "syncop");
        String path = "syncop.fields";
        String op = f.string("op", path);
        String syncType = f.string("synctyp", path);
        int wantArgs = switch (syncType + "." + op) {
            case "Mutex.Lock", "Mutex.Unlock",
                 "RWMutex.Lock", "RWMutex.Unlock", "RWMutex.RLock", "RWMutex.RUnlock",
                 "WaitGroup.Done", "WaitGroup.Wait",
                 // P3-M11 (sync.Pool).
                 "Pool.Get", "Pool.GetNew", "Pool.CallNew" -> 0;
            case "WaitGroup.Add", "Once.Do",
                 "Pool.Put", "Pool.SetNew" -> 1;
            default -> throw new FrontendException(path + ": unknown sync op " + syncType + "." + op);
        };
        GoExprNode recv = factory.buildExpr(f.object("recv", path), ctx);
        Json.Array arr = f.array("args", path);
        if (arr.size() != wantArgs) {
            throw new FrontendException(
                    path + ": " + syncType + "." + op + " takes " + wantArgs + " arguments");
        }
        GoExprNode[] args = new GoExprNode[arr.size()];
        for (int i = 0; i < args.length; i++) {
            Json.Value v = arr.get(i);
            if (!(v instanceof Json.Object o)) {
                throw new FrontendException(path + ".args[" + i + "]: must be a node object");
            }
            args[i] = factory.buildExpr(o, ctx);
        }
        // P3-M11: Pool.Put's parameter is declared `any` — the wire
        // carries the concrete arg node, so box it to an interface value
        // exactly as implicit call-arg conversions do (the atomic.Value
        // precedent in AtomicOpNode).
        if (syncType.equals("Pool") && op.equals("Put")) {
            args[0] = factory.boxToInterface(args[0], ctx, path + ".args[0]");
        }
        GoKind resultKind = RESULT_KINDS.getOrDefault(syncType + "." + op, GoKind.INTERFACE);
        return new SyncOpNode(op, syncType, resultKind, recv, args);
    }
}
