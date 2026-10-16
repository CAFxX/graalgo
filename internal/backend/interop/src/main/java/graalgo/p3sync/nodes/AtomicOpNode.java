package graalgo.p3sync.nodes;

import java.util.Map;
import graalgo.runtime.sync.IntCell;
import graalgo.runtime.sync.LongCell;
import java.util.concurrent.atomic.AtomicReference;

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
 * method. The receiver evaluates to the {@link LongCell} /
 * {@link IntCell} cell itself (ADR-0002 D1: the Go value IS the
 * Java object); the implementation delegates to
 * {@link graalgo.runtime.Atomics} (the real phase-1 atomics).
 *
 * <p>P3-M10: the generic {@code Pointer[T]} and the any-typed
 * {@code Value} ride on an {@link AtomicReference} cell. A Go
 * {@code *T} result is {@code POINTER} kind (an opaque Java reference;
 * a zero Pointer loads null, i.e. Go's nil {@code *T}); a Go
 * {@code any} result is {@code INTERFACE} kind.
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
            Map.entry("atomic.Int64.And", GoKind.INT64),
            Map.entry("atomic.Int64.Or", GoKind.INT64),
            Map.entry("atomic.Int64.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Uint64.Load", GoKind.UINT64),
            Map.entry("atomic.Uint64.Add", GoKind.UINT64),
            Map.entry("atomic.Uint64.Swap", GoKind.UINT64),
            Map.entry("atomic.Uint64.And", GoKind.UINT64),
            Map.entry("atomic.Uint64.Or", GoKind.UINT64),
            Map.entry("atomic.Uint64.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Int32.Load", GoKind.INT32),
            Map.entry("atomic.Int32.Add", GoKind.INT32),
            Map.entry("atomic.Int32.Swap", GoKind.INT32),
            Map.entry("atomic.Int32.And", GoKind.INT32),
            Map.entry("atomic.Int32.Or", GoKind.INT32),
            Map.entry("atomic.Int32.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Uint32.Load", GoKind.UINT32),
            Map.entry("atomic.Uint32.Add", GoKind.UINT32),
            Map.entry("atomic.Uint32.Swap", GoKind.UINT32),
            Map.entry("atomic.Uint32.And", GoKind.UINT32),
            Map.entry("atomic.Uint32.Or", GoKind.UINT32),
            Map.entry("atomic.Uint32.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Bool.Load", GoKind.BOOL),
            Map.entry("atomic.Bool.Swap", GoKind.BOOL),
            Map.entry("atomic.Bool.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Uintptr.Load", GoKind.UINT64),
            Map.entry("atomic.Uintptr.Add", GoKind.UINT64),
            Map.entry("atomic.Uintptr.Swap", GoKind.UINT64),
            Map.entry("atomic.Uintptr.And", GoKind.UINT64),
            Map.entry("atomic.Uintptr.Or", GoKind.UINT64),
            Map.entry("atomic.Uintptr.CompareAndSwap", GoKind.BOOL),
            // P3-M10: Pointer[T] (Go *T results are POINTER kind) and
            // Value (Go any results are INTERFACE kind).
            Map.entry("atomic.Pointer.Load", GoKind.POINTER),
            Map.entry("atomic.Pointer.Swap", GoKind.POINTER),
            Map.entry("atomic.Pointer.CompareAndSwap", GoKind.BOOL),
            Map.entry("atomic.Value.Load", GoKind.INTERFACE),
            Map.entry("atomic.Value.Swap", GoKind.INTERFACE),
            Map.entry("atomic.Value.CompareAndSwap", GoKind.BOOL));

    /** Expected D5 argument count per op. */
    private static final Map<String, Integer> ARG_COUNTS = Map.of(
            "Load", 0, "Store", 1, "Add", 1, "Swap", 1, "And", 1, "Or", 1,
            "CompareAndSwap", 2);

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
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        return r;
    }

    private LongCell cell64(VirtualFrame frame) {
        Object r = cell(frame);
        if (!(r instanceof LongCell c)) {
            throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " on non-LongCell cell: "
                            + r.getClass().getName());
        }
        return c;
    }

    private IntCell cell32(VirtualFrame frame) {
        Object r = cell(frame);
        if (!(r instanceof IntCell c)) {
            throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " on non-IntCell cell: "
                            + r.getClass().getName());
        }
        return c;
    }

    @SuppressWarnings("unchecked")
    private AtomicReference<Object> cellRef(VirtualFrame frame) {
        Object r = cell(frame);
        if (!(r instanceof AtomicReference<?> c)) {
            throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " on non-AtomicReference cell: "
                            + r.getClass().getName());
        }
        return (AtomicReference<Object>) c;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return switch (atomicType + "." + op) {
            case "atomic.Int64.Load" -> Atomics.loadInt64(cell64(frame));
            case "atomic.Int64.Add" -> Atomics.addInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Int64.Swap" -> Atomics.swapInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Int64.And" -> Atomics.andInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Int64.Or" -> Atomics.orInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.Load" -> Atomics.loadInt64(cell64(frame));
            case "atomic.Uint64.Add" -> Atomics.addInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.Swap" -> Atomics.swapInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.And" -> Atomics.andInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.Or" -> Atomics.orInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.Load" -> Atomics.loadUintptr(cell64(frame));
            case "atomic.Uintptr.Add" -> Atomics.addUintptr(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.Swap" -> Atomics.swapUintptr(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.And" -> Atomics.andInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.Or" -> Atomics.orInt64(cell64(frame), args[0].executeLong(frame));
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
            case "atomic.Int32.And" -> Atomics.andInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Int32.Or" -> Atomics.orInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.Load" -> Atomics.loadInt32(cell32(frame));
            case "atomic.Uint32.Add" -> Atomics.addInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.Swap" -> Atomics.swapInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.And" -> Atomics.andInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.Or" -> Atomics.orInt32(cell32(frame), args[0].executeInt(frame));
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
            // P3-M10: reference-cell CAS with Go equality semantics
            // (Pointer[T] compares addresses via GoPointer.sameStorageAs;
            // Value compares interface values via GoEquality) — Java
            // reference identity is wrong for both (&x evaluated twice
            // yields two wrappers; separately boxed equal anys differ).
            case "atomic.Pointer.CompareAndSwap" -> Atomics.compareAndSwapGoPointer(cellRef(frame),
                    args[0].executeObject(frame), args[1].executeObject(frame));
            case "atomic.Value.CompareAndSwap" -> Atomics.compareAndSwapGoValue(cellRef(frame),
                    args[0].executeObject(frame), args[1].executeObject(frame));
            default -> throw new FrontendException(
                    "atomic op " + atomicType + "." + op + " is not a boolean op");
        };
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // Only the void Store reaches here (kind INTERFACE), plus the
        // POINTER/INTERFACE-valued Load/Swap of Pointer[T]/Value.
        switch (atomicType + "." + op) {
            case "atomic.Int64.Store" -> Atomics.storeInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uint64.Store" -> Atomics.storeInt64(cell64(frame), args[0].executeLong(frame));
            case "atomic.Uintptr.Store" -> Atomics.storeUintptr(cell64(frame), args[0].executeLong(frame));
            case "atomic.Int32.Store" -> Atomics.storeInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Uint32.Store" -> Atomics.storeInt32(cell32(frame), args[0].executeInt(frame));
            case "atomic.Bool.Store" -> Atomics.storeBool(cell32(frame), args[0].executeBoolean(frame));
            // P3-M10: Pointer[T] (*T is an opaque Java reference; a zero
            // Pointer loads null, which is Go's nil *T) and Value (any).
            case "atomic.Pointer.Load" -> {
                return Atomics.loadPointer(cellRef(frame));
            }
            case "atomic.Pointer.Store" -> {
                Atomics.storePointer(cellRef(frame), args[0].executeObject(frame));
                return null;
            }
            case "atomic.Pointer.Swap" -> {
                return Atomics.swapPointer(cellRef(frame), args[0].executeObject(frame));
            }
            case "atomic.Value.Load" -> {
                return Atomics.loadPointer(cellRef(frame));
            }
            case "atomic.Value.Store" -> {
                Atomics.storePointer(cellRef(frame), args[0].executeObject(frame));
                return null;
            }
            case "atomic.Value.Swap" -> {
                return Atomics.swapPointer(cellRef(frame), args[0].executeObject(frame));
            }
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
        // P3-M10: atomic.Value's operands are declared `any` — the wire
        // carries the concrete arg nodes, so box them to interface values
        // exactly as implicit call-arg conversions do. (The typed
        // wrappers' params are never interfaces; Pointer[T]'s are *T.)
        // Load takes no args; Store/Swap/CompareAndSwap take only anys.
        if (synctyp.equals("atomic.Value") && !"Load".equals(op)) {
            for (int i = 0; i < args.length; i++) {
                args[i] = factory.boxToInterface(args[i], ctx, path + ".args[" + i + "]");
            }
        }
        return new AtomicOpNode(op, synctyp, resultKind, recv, args);
    }
}
