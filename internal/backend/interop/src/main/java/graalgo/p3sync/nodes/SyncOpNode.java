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
import graalgo.runtime.sync.GoSyncMap;
import graalgo.values.GoInterface;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
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
 * Misuse semantics live in the primitives themselves (P1-9, ADR-0002 D4):
 * <ul>
 *   <li>{@code sync: negative WaitGroup counter} (WaitGroup.Add and
 *       WaitGroup.Done) — the primitive throws a recoverable Go panic
 *       via {@link Panic#throwPanic}: full gc parity;</li>
 *   <li>{@code sync: unlock of unlocked mutex} /
 *       {@code sync: Unlock of unlocked RWMutex} /
 *       {@code sync: RUnlock of unlocked RWMutex} — the primitives throw
 *       {@code graalgo.runtime.GoFatalError}: gc <em>fatal-errors</em>
 *       ({@code fatal error: ...}, unrecoverable, exit 2), and so do we.
 *       Fatal errors propagate through deferred functions untouched
 *       (only {@code PanicException} is recoverable).</li>
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
 * <p>fix-linkname (Caddy/godebug lane): {@code sync.Map}. The four
 * multi-result methods (Load, LoadOrStore, LoadAndDelete, Swap) return
 * the {@code mapAnyBoolPair{v, ok}} struct for the mrSyncMap desugar
 * (the node kind is {@link GoKind#STRUCT} for those ops); the
 * {@code any}-typed arguments are boxed to {@link GoInterface} at
 * build time, exactly like call-site adaptation. {@code Range}
 * invokes the Go callback per entry (with the
 * {@code FuncValueCallNode} recoverAllowed dance, like
 * {@code Once.Do}); entries deleted mid-iteration are skipped, per the
 * Go spec.
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
     * Non-null for the pair-returning {@code sync.Map} ops (Load,
     * LoadOrStore, LoadAndDelete, Swap): the {@code mapAnyBoolPair}
     * descriptor for building the result struct.
     */
    private final GoStructDescriptor pairDesc;

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

    private SyncOpNode(String op, String syncType, GoExprNode recv, GoExprNode[] args,
            GoKind kind, GoStructDescriptor pairDesc) {
        super(kind);
        this.op = op;
        this.syncType = syncType;
        this.recv = recv;
        this.args = args;
        this.pairDesc = pairDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object r = recv.executeObject(frame);
        if (r == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        switch (syncType + "." + op) {
            case "Mutex.Lock" -> ((GoMutex) r).lock();
            case "Mutex.Unlock" -> ((GoMutex) r).unlock();
            case "RWMutex.Lock" -> ((GoRWMutex) r).lock();
            case "RWMutex.Unlock" -> ((GoRWMutex) r).unlock();
            case "RWMutex.RLock" -> ((GoRWMutex) r).rLock();
            case "RWMutex.RUnlock" -> ((GoRWMutex) r).rUnlock();
            case "WaitGroup.Add" -> ((GoWaitGroup) r).add((int) args[0].executeLong(frame));
            case "WaitGroup.Done" -> ((GoWaitGroup) r).done();
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
            // fix-linkname: sync.Map. The any-typed arguments were boxed
            // to GoInterface at build time; the pair ops return the
            // mapAnyBoolPair struct for the mrSyncMap desugar.
            case "Map.Load" -> { return mapPair((GoSyncMap) r,
                    ((GoSyncMap) r).load(args[0].executeObject(frame))); }
            case "Map.Store" -> ((GoSyncMap) r).store(
                    args[0].executeObject(frame), args[1].executeObject(frame));
            case "Map.LoadOrStore" -> { return mapPair((GoSyncMap) r,
                    ((GoSyncMap) r).loadOrStore(
                            args[0].executeObject(frame), args[1].executeObject(frame))); }
            case "Map.LoadAndDelete" -> { return mapPair((GoSyncMap) r,
                    ((GoSyncMap) r).loadAndDelete(args[0].executeObject(frame))); }
            case "Map.Delete" -> ((GoSyncMap) r).delete(args[0].executeObject(frame));
            case "Map.Swap" -> { return mapPair((GoSyncMap) r,
                    ((GoSyncMap) r).swap(
                            args[0].executeObject(frame), args[1].executeObject(frame))); }
            case "Map.Range" -> doRange((GoSyncMap) r, frame);
            case "Map.Clear" -> ((GoSyncMap) r).clear();
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

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        Object r = recv.executeObject(frame);
        if (r == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        // Only the boolean sync.Map ops reach here (kind BOOL).
        return switch (syncType + "." + op) {
            case "Map.CompareAndSwap" -> ((GoSyncMap) r).compareAndSwap(args[0].executeObject(frame),
                    args[1].executeObject(frame), args[2].executeObject(frame));
            case "Map.CompareAndDelete" -> ((GoSyncMap) r).compareAndDelete(
                    args[0].executeObject(frame), args[1].executeObject(frame));
            default -> throw new FrontendException(
                    "sync op " + syncType + "." + op + " is not a boolean op");
        };
    }

    /**
     * Builds the {@code mapAnyBoolPair{v, ok}} result struct for the
     * multi-result {@code sync.Map} ops. The stored value is already a
     * {@link GoInterface} (boxed at build time); a missing value yields
     * the nil interface, matching gc.
     */
    private GoStruct mapPair(GoSyncMap m, GoSyncMap.LoadResult lr) {
        if (pairDesc == null) {
            throw new FrontendException("sync.Map." + op + ": missing pair descriptor");
        }
        int vIdx = -1;
        int okIdx = -1;
        for (GoStructDescriptor.Field f : pairDesc.fields()) {
            if (f.name().equals("v") && !f.inLongs()) {
                vIdx = f.index();
            } else if (f.name().equals("ok") && f.inLongs()) {
                okIdx = f.index();
            }
        }
        if (vIdx < 0 || okIdx < 0) {
            throw new FrontendException(
                    "sync.Map." + op + ": mapAnyBoolPair needs v (objects) and ok (longs) fields");
        }
        long[] longs = new long[pairDesc.longsCount()];
        Object[] objs = new Object[pairDesc.objectsCount()];
        // The value rides through even when ok is false (LoadOrStore on a
        // new key returns the stored value); a Java-null value means
        // absent (load/loadAndDelete/swap miss) → the nil interface.
        objs[vIdx] = lr.value() != null ? GoInterface.narrow(lr.value()) : GoInterface.NIL;
        longs[okIdx] = lr.ok() ? 1 : 0;
        return new GoStruct(pairDesc, longs, objs);
    }

    /**
     * {@code Map.Range(f)}: invokes the Go callback for each entry. The
     * callback runs with the {@code FuncValueCallNode} recoverAllowed
     * dance (like {@code Once.Do}); a false return stops the iteration.
     * Entries deleted mid-iteration are skipped (Go spec).
     */
    private Object doRange(GoSyncMap m, VirtualFrame frame) {
        Object fv = args[0].executeObject(frame);
        if (!(fv instanceof GoFunc f)) {
            Panic.throwPanic(GoCalls.nilFuncCall(), null, null, this);
            return null;
        }
        GoroutineState state = GoroutineState.current();
        boolean prev = state != null && state.isRecoverAllowed();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        try {
            for (Object k : m.keysSnapshot()) {
                if (!m.containsKey(k)) {
                    continue;
                }
                Object v = m.get(k);
                Object cont = GoCalls.invoke(f, new Object[]{k, v});
                if (cont instanceof Boolean b) {
                    if (!b) {
                        break;
                    }
                } else {
                    throw new FrontendException(
                            "sync.Map.Range callback returned non-bool: "
                                    + (cont == null ? "null" : cont.getClass().getName()));
                }
            }
        } finally {
            if (state != null) {
                state.setRecoverAllowed(prev);
            }
        }
        return null;
    }

    /**
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
        String key = syncType + "." + op;
        int wantArgs = switch (key) {
            case "Mutex.Lock", "Mutex.Unlock",
                 "RWMutex.Lock", "RWMutex.Unlock", "RWMutex.RLock", "RWMutex.RUnlock",
                 "WaitGroup.Done", "WaitGroup.Wait",
                 // P3-M11 (sync.Pool).
                 "Pool.Get", "Pool.GetNew", "Pool.CallNew",
                 // fix-linkname: sync.Map.
                 "Map.Clear" -> 0;
            case "WaitGroup.Add", "Once.Do",
                 "Pool.Put", "Pool.SetNew",
                 // fix-linkname: sync.Map.
                 "Map.Load", "Map.LoadAndDelete", "Map.Delete", "Map.Range" -> 1;
            case "Map.Store", "Map.LoadOrStore", "Map.Swap", "Map.CompareAndDelete" -> 2;
            case "Map.CompareAndSwap" -> 3;
            default -> throw new FrontendException(path + ": unknown sync op " + key);
        };
        GoExprNode recv = factory.buildExpr(f.object("recv", path), ctx);
        Json.Array arr = f.array("args", path);
        if (arr.size() != wantArgs) {
            throw new FrontendException(
                    path + ": " + key + " takes " + wantArgs + " arguments");
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
        // fix-linkname: sync.Map's any-typed arguments are boxed to
        // GoInterface here (call-site adaptation for any params); the
        // Range callback is func-typed and stays unboxed.
        if (syncType.equals("Map") && !op.equals("Range") && !op.equals("Clear")) {
            for (int i = 0; i < args.length; i++) {
                args[i] = factory.boxToInterface(args[i], ctx, path + ".args[" + i + "]");
            }
        }
        // The pair-returning Map ops (mrSyncMap desugar) build the
        // mapAnyBoolPair struct: STRUCT kind + descriptor from the wire
        // "type" field. The boolean ops are BOOL kind. Pool ops use
        // RESULT_KINDS.
        GoKind kind = RESULT_KINDS.getOrDefault(key, GoKind.INTERFACE);
        GoStructDescriptor pairDesc = null;
        // fix-linkname: Map ops override the kind.
        switch (key) {
            case "Map.Load", "Map.LoadOrStore", "Map.LoadAndDelete", "Map.Swap" -> {
                kind = GoKind.STRUCT;
                Json.Value tv = f.opt("type");
                if (!(tv instanceof Json.Num num)) {
                    throw new FrontendException(
                            path + ": " + key + " needs a numeric \"type\" id (the pair type)");
                }
                int typeId = (int) num.asLong(path + ".type");
                if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.STRUCT) {
                    throw new FrontendException(
                            path + ": " + key + " pair type must be a struct kind");
                }
                pairDesc = NodeFactory.structDescriptorFor(ctx, typeId, path);
            }
            case "Map.CompareAndSwap", "Map.CompareAndDelete" -> kind = GoKind.BOOL;
            default -> { /* keep RESULT_KINDS or INTERFACE */ }
        }
        return new SyncOpNode(op, syncType, recv, args, kind, pairDesc);
    }
}
