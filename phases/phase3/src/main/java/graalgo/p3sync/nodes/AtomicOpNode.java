package graalgo.p3sync.nodes;

import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.Atomics;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;

/**
 * An operation on a {@code sync/atomic} typed wrapper (P3-M4, WS C,
 * ADR-0004 D2): the {@code "syncop"} wire node with an
 * {@code "atomic.<Name>"} {@code synctyp}.
 *
 * <p>Unlike {@link SyncOpNode} (whose ops are all void), atomic ops
 * return values, so this node carries the real result kind
 * ({@code INT64}/{@code INT32}/{@code BOOL}; {@code INTERFACE} for the
 * void {@code Store}) and implements the kind-appropriate execute
 * method. The receiver evaluates to the {@link AtomicLong} /
 * {@link AtomicInteger} cell itself (ADR-0002 D1: the Go value IS the
 * Java object); the implementation delegates to
 * {@link graalgo.runtime.Atomics} (the real phase-1 atomics).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1
 * {@code NodeFactory} change. The P3 launcher's {@code "syncop"}
 * registration dispatches {@code "atomic.*"} here.
 *
 * <p>verifies: GG-0020
 */
public final class AtomicOpNode extends GoExprNode {

    /** "atomic.Int64.Load" → result kind; Store (void) is absent. */
    private static final Map<String, GoKind> RESULT_KINDS = Map.ofEntries(
            Map.entry("atomic.Int64.Load", GoKind.INT64),
            Map.entry("atomic.Int64.Add", GoKind.INT64),
            Map.entry("atomic.Int64.Swap", GoKind.INT64),
            Map.entry("atomic.Int64.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Uint64.Load", GoKind.UINT64),
            Map.entry("atomic.Uint64.Add", GoKind.UINT64),
            Map.entry("atomic.Uint64.Swap", GoKind.UINT64),
            Map.entry("atomic.Uint64.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Int32.Load", GoKind.INT32),
            Map.entry("atomic.Int32.Add", GoKind.INT32),
            Map.entry("atomic.Int32.Swap", GoKind.INT32),
            Map.entry("atomic.Int32.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Uint32.Load", GoKind.UINT32),
            Map.entry("atomic.Uint32.Add", GoKind.UINT32),
            Map.entry("atomic.Uint32.Swap", GoKind.UINT32),
            Map.entry("atomic.Uint32.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Bool.Load", GoKind.BOOL),
            Map.entry("atomic.Bool.Swap", GoKind.BOOL),
            Map.entry("atomic.Bool.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Uintptr.Load", GoKind.UINT64),
            Map.entry("atomic.Uintptr.Add", GoKind.UINT64),
            Map.entry("atomic.Uintptr.Swap", GoKind.UINT64),
            Map.entry("atomic.Uintptr.CompareAndSwap", GoKind.BOOL));

    /** Expected D5 argument count per op. */
    private static final Map<String, Integer> ARG_COUNTS = Map.of(
            "Load", 0, "Store", 1, "Add", 1, "Swap", 1, "CompareAndSwap", 2);

    private final String op;
    private final String atomicType;

    @Child
    private GoExprNode recv;
    @Children
    private GoExprNode[] args;

    private AtomicOpNode(String op, String atomicType, GoKind resultKind,
            GoExprNode recv, GoExprNode[] args) {
        super(resultKind);
        this.op = op;
        this.atomicType = atomicType;
        this.recv = recv;
        this.args = args;
    }

    private Object cell(VirtualFrame frame) {
        Object r = recv.executeObject(frame);
        if (r == null) {
            Panic.throwPanic(GoErrors.nilDereference());
        }
        return r;
    }

    private AtomicLong cell64(VirtualFrame frame) {
        Object r = cell(frame);
        if (!(r instanceof AtomicLong c)) {
            throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " on non-AtomicLong cell: "
                            + r.getClass().getName());
        }
        return c;
    }

    private AtomicInteger cell32(VirtualFrame frame) {
        Object r = cell(frame);
        if (!(r instanceof AtomicInteger c)) {
            throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " on non-AtomicInteger cell: "
                            + r.getClass().getName());
        }
        return c;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (atomicType + "." + op) {
            case "atomic.Int64.Load" -> Atomics.loadInt64(cell64(frame));
            case "atomic.Int64.Add" -> Atomics.addInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Int64.Swap" -> Atomics.swapInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.Load" -> Atomics.loadInt64(cell64(frame));
            case "atomic.Uint64.Add" -> Atomics.addInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.Swap" -> Atomics.swapInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.Load" -> Atomics.loadUintptr(cell64(frame));
            case "atomic.Uintptr.Add" -> Atomics.addUintptr(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.Swap" -> Atomics.swapUintptr(cell64(frame), args[0].executeLong(frame));
            default -> throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " is not a long op");
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return switch (atomicType + "." + op) {
            case "atomic.Int32.Load" -> Atomics.loadInt32(cell32(frame));
            case "atomic.Int32.Add" -> Atomics.addInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Int32.Swap" -> Atomics.swapInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.Load" -> Atomics.loadInt32(cell32(frame));
            case "atomic.Uint32.Add" -> Atomics.addInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.Swap" -> Atomics.swapInt32(cell32(frame), args[0].executeInt(frame));
            default -> throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " is not an int op");
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return switch (atomicType + "." + op) {
            case "atomic.Int64.CompareAndSwap" -> Atomics.compareAndSwapInt64(cell64(frame),
                    args[0].executeLong(frame), args[1].executeLong(frame));
            case "atomic.Uint64.CompareAndSwap" -> Atomics.compareAndSwapInt64(cell64(frame),
                    args[0].executeLong(frame), args[1].executeLong(frame));
            case "atomic.Uintptr.CompareAndSwap" -> Atomics.compareAndSwapUintptr(cell64(frame),
                    args[0].executeLong(frame), args[1].executeLong(frame));
            case "atomic.Int32.CompareAndSwap" -> Atomics.compareAndSwapInt32(cell32(frame),
                    args[0].executeInt(frame), args[1].executeInt(frame));
            case "atomic.Uint32.CompareAndSwap" -> Atomics.compareAndSwapInt32(cell32(frame),
                    args[0].executeInt(frame), args[1].executeInt(frame));
            case "atomic.Bool.Load" -> Atomics.loadBool(cell32(frame));
            case "atomic.Bool.Swap" -> Atomics.swapBool(cell32(frame), args[0].executeBoolean(frame));
            case "atomic.Bool.CompareAndSwap" -> Atomics.compareAndSwapBool(cell32(frame),
                    args[0].executeBoolean(frame), args[1].executeBoolean(frame));
            default -> throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " is not a boolean op");
        };
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // Only the void Store reaches here (kind INTERFACE).
        switch (atomicType + "." + op) {
            case "atomic.Int64.Store" -> Atomics.storeInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.Store" -> Atomics.storeInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.Store" -> Atomics.storeUintptr(cell64(frame), args[0].executeLong(frame));
            case "atomic.Int32.Store" -> Atomics.storeInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.Store" -> Atomics.storeInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Bool.Store" -> Atomics.storeBool(cell32(frame), args[0].executeBoolean(frame));
            default -> throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " is not a void op");
        }
        return null;
    }

    /**
     * Builds the node from the {@code "syncop"} wire node with an
     * {@code "atomic.*"} synctyp. Fields: {@code op}, {@code synctyp},
     * {@code recv}, {@code args}, {@code type} (the result type id; null
     * for the void {@code Store}).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "syncop");
        String path = "syncop.fields";
        String op = f.string("op", path);
        String synctyp = f.string("synctyp", path);
        String key = synctyp + "." + op;
        Integer wantArgs = ARG_COUNTS.get(op);
        if (wantArgs == null) {
            throw new FrontendException(path + ": unknown atomic op " + key
                    + " (not in M4 scope)");
        }
        GoKind resultKind = RESULT_KINDS.get(key);
        if (resultKind == null && !"Store".equals(op)) {
            throw new FrontendException(path + ": unknown atomic op " + key
                    + " (not in M4 scope)");
        }
        if (resultKind == null) {
            resultKind = GoKind.INTERFACE; // void Store
        }
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
        return new AtomicOpNode(op, synctyp, resultKind, recv, args);
    }
}
