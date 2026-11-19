package graalgo.p3sync.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NilNode;
import graalgo.frontend.NodeFactory;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

import java.util.concurrent.atomic.AtomicReference;

/**
 * Package-level {@code sync/atomic} pointer operations
 * (fix-redmaster-r39; load/store added by P1-1,
 * REVIEW-2026-11-03-09): the {@code "atomicptrop"} wire node, lowering
 * {@code atomic.LoadPointer} ({@code op="load"}, result the
 * {@code unsafe.Pointer}), {@code atomic.StorePointer} ({@code op="store"},
 * result the stored handle, discarded by the frontend's
 * {@code exprstmt} wrapper), {@code atomic.SwapPointer}
 * ({@code op="swap"}, result the old {@code unsafe.Pointer}) and
 * {@code atomic.CompareAndSwapPointer} ({@code op="cas"}, result bool).
 * The frontend emits it for both the qualified forms and the
 * unqualified calls inside {@code sync/atomic} itself (the
 * {@code atomic.Pointer[T]} method bodies in type.go call the bodyless
 * assembly declarations, which previously reached the
 * unimplemented-function panic).
 *
 * <p>The addressed word holds an {@code unsafe.Pointer} handle (a
 * {@code long}), the representation the compiled {@code Pointer[T]}
 * bodies themselves use through {@code &x.v} deref reads/writes. Two
 * storage shapes are supported:
 *
 * <ul>
 *   <li>an {@link AtomicReference} cell (an {@code atomic.Pointer} /
 *   {@code atomic.Value} value — {@code &x.v} yields the cell itself,
 *   see {@code FieldAddrNode}): the ops run on the cell with the
 *   volatile {@code AtomicReference} operations (load {@code get},
 *   store {@code set}, swap/CAS loops), giving gc's acquire/release
 *   semantics directly. A cell content written by another path as a
 *   {@link GoPointer} is normalized to its handle for comparison and
 *   replaced in handle form on write (the load/store ops share this
 *   representation: a cell store writes the handle, exactly as swap/CAS
 *   already did, so every {@code atomicptrop} reader normalizes the
 *   same way).</li>
 *   <li>a plain {@link GoPointer} to an {@code unsafe.Pointer} field:
 *   all four ops serialize on the single shared {@code LOCK} below.
 *   That shared lock is the ordering point: a store's monitor exit
 *   happens-before a later load's monitor entry on the same lock, so
 *   a {@code StorePointer} (release) synchronizes with a
 *   {@code LoadPointer} (acquire) observing it, and plain writes
 *   sequenced before the store are visible to plain reads sequenced
 *   after the load — the gc happens-before guarantee the old plain
 *   deref load/store silently lacked. The remaining scoped divergence
 *   is progress only (lock-serialized, not lock-free), never ordering.
 *   Mixed atomic/plain access to the pointer word itself is a data
 *   race under gc's memory model and is out of scope, as are plain
 *   accesses from other threads, which do not take this lock.</li>
 * </ul>
 *
 * <p>Handle equality is pointer identity (PKG-007): two handles are
 * equal iff they name the same storage, so a handle-valued CAS matches
 * gc's address CAS on every representable case.
 *
 * <p>Nil addresses panic with gc's nil-dereference panic for all four
 * ops (the old deref lowering's behavior, kept for load/store;
 * R2315-14-P1-3 extends it to swap/CAS, whose plain-field paths
 * silently succeeded on a nil address reached through the unsafe
 * round-trip {@code (*unsafe.Pointer)(unsafe.Pointer(nil))}). The
 * panic fires only after the value arguments have been evaluated
 * (gc's call-argument order): the F1 fixup
 * (REVIEW-2026-11-19-nilptr-db05e17a) reordered the nil check behind
 * the value evaluation for swap/store/CAS, so a nil address given as
 * a nil variable or literal no longer skips the value side effects.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class AtomicPtrOpNode extends GoExprNode {
    /** Shared RMW lock for plain-field storage (see class javadoc). */
    private static final Object LOCK = new Object();

    private final String op;
    @Child private GoExprNode addrChild;
    @Children private GoExprNode[] argChildren;

    private AtomicPtrOpNode(String op, GoExprNode addrChild, GoExprNode[] argChildren) {
        super("cas".equals(op) ? GoKind.BOOL : GoKind.UNSAFE_POINTER);
        this.op = op;
        this.addrChild = addrChild;
        this.argChildren = argChildren;
    }

    /**
     * The addressed storage, unwrapped to a cell or a plain pointer.
     * Returns the raw address object WITHOUT panicking: a nil address
     * reaches this node in two forms — a Java null (a nil
     * {@code *unsafe.Pointer} variable or literal) and a nil
     * {@code GoPointer} (the unsafe round-trip
     * {@code (*unsafe.Pointer)(unsafe.Pointer(nil))}) — and the nil
     * check ({@link #requireNonNil}) runs only AFTER the value
     * arguments have been evaluated, so their side effects are not
     * skipped (gc evaluates call arguments before the callee panics;
     * F1 fixup, REVIEW-2026-11-19-nilptr-db05e17a).
     */
    private Object addressObject(VirtualFrame frame) {
        return addrChild.executeObject(frame);
    }

    /** Normalizes a stored cell content to its handle value. */
    private static long toHandle(Object stored) {
        if (stored == null) {
            return 0;
        }
        if (stored instanceof Number n) {
            return n.longValue();
        }
        if (stored instanceof GoPointer gp) {
            return UnsafeTable.handleForPointer(gp);
        }
        throw new FrontendException("atomicptrop: unsupported stored value "
                + stored.getClass().getName());
    }

    private static long readPlain(GoPointer p) {
        if (p.isNil()) {
            return 0;
        }
        Object r = p.referent();
        if (r == null) {
            return 0;
        }
        if (r instanceof Number n) {
            return n.longValue();
        }
        if (r instanceof GoPointer gp) {
            return UnsafeTable.handleForPointer(gp);
        }
        throw new FrontendException("atomicptrop: plain field holds a non-handle value "
                + r.getClass().getName());
    }

    /**
     * Nil-address check for all four ops: gc's nil-dereference panic.
     * The address reaches this node in two forms — a Java null (a nil
     * {@code *unsafe.Pointer} variable or literal as the address) and
     * a nil {@code GoPointer} (the unsafe round-trip
     * {@code (*unsafe.Pointer)(unsafe.Pointer(nil))}) — and both must
     * panic. Callers invoke this AFTER evaluating the value
     * arguments: gc evaluates call arguments before the callee
     * panics, so a check placed before the value evaluation would
     * silently skip the value side effects (the R2315-14-P2-4 shape).
     * A cell address is never nil, so the check is a no-op there.
     */
    private void requireNonNil(Object s) {
        if (s == null || (s instanceof GoPointer p && p.isNil())) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            throw new AssertionError("unreachable");
        }
    }

    /** Load: the current handle. Cell reads are volatile; plain-field reads take LOCK. */
    private long loadHandle(VirtualFrame frame) {
        Object s = addressObject(frame);
        requireNonNil(s);
        if (s instanceof AtomicReference<?> ar) {
            return toHandle(ar.get());
        }
        if (s instanceof GoPointer p) {
            synchronized (LOCK) {
                return readPlain(p);
            }
        }
        throw new FrontendException("atomicptrop: address is neither a cell nor a pointer: "
                + s.getClass().getName());
    }

    /**
     * Store: writes the handle, returns it (the frontend discards it via
     * {@code exprstmt}). Cell writes are volatile; plain-field writes take
     * LOCK — the same lock the load takes, which is what makes this a
     * release to the load's acquire. The caller evaluates the address
     * before the value (Go's left-to-right call-argument order, as in
     * the swap path) and passes the already-evaluated storage in.
     */
    private long storeHandle(Object s, long newVal) {
        requireNonNil(s);
        if (s instanceof AtomicReference<?> ar) {
            @SuppressWarnings("unchecked")
            AtomicReference<Object> cell = (AtomicReference<Object>) ar;
            cell.set(Long.valueOf(newVal));
            return newVal;
        }
        if (s instanceof GoPointer p) {
            synchronized (LOCK) {
                p.setReferent(Long.valueOf(newVal));
                return newVal;
            }
        }
        throw new FrontendException("atomicptrop: address is neither a cell nor a pointer: "
                + s.getClass().getName());
    }

    /**
     * Reads a value argument as a handle. An untyped {@code nil} lowers
     * to an INTERFACE-kind {@link NilNode}, which has no long value; a
     * nil pointer is the zero word (0). Every other argument is a
     * long-valued handle expression. (R2315-13-P1-1: without this, a nil
     * value argument to store/swap/CAS died with
     * UnsupportedOperationException.) Address arguments never go through
     * here: a nil address must panic (F1), and {@link #requireNonNil}
     * handles it after the value arguments are evaluated.
     */
    private static long handleArg(GoExprNode arg, VirtualFrame frame) {
        if (arg instanceof NilNode) {
            return 0L;
        }
        return arg.executeLong(frame);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // load: no value argument. store/swap: one. (cas is boolean-kind
        // and goes through executeBoolean below.)
        switch (op) {
            case "load" -> {
                return loadHandle(frame);
            }
            case "store" -> {
                Object s = addressObject(frame);
                long newVal = handleArg(argChildren[0], frame);
                return storeHandle(s, newVal);
            }
            case "swap" -> {
                // swap: returns the old handle.
                Object s = addressObject(frame);
                long newVal = handleArg(argChildren[0], frame);
                // R2315-14-P1-3: a nil address must panic, never
                // silently succeed. The check sits after the value
                // evaluation (gc evaluates call arguments before the
                // callee panics — the store path has the same order)
                // and before the storage-shape dispatch, so it covers
                // both the cell and the plain-field shapes (a cell
                // address is never nil, so it is a no-op there).
                requireNonNil(s);
                if (s instanceof AtomicReference<?> ar) {
                    @SuppressWarnings("unchecked")
                    AtomicReference<Object> cell = (AtomicReference<Object>) ar;
                    while (true) {
                        Object cur = cell.get();
                        if (cell.compareAndSet(cur, Long.valueOf(newVal))) {
                            return toHandle(cur);
                        }
                    }
                }
                if (s instanceof GoPointer p) {
                    synchronized (LOCK) {
                        long old = readPlain(p);
                        p.setReferent(Long.valueOf(newVal));
                        return old;
                    }
                }
                throw new FrontendException("atomicptrop: address is neither a cell nor a pointer: "
                        + s.getClass().getName());
            }
            default -> throw new FrontendException("atomicptrop: unknown op " + op);
        }
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        // cas: compares the stored handle against old, stores new on match.
        Object s = addressObject(frame);
        long oldVal = handleArg(argChildren[0], frame);
        long newVal = handleArg(argChildren[1], frame);
        // R2315-14-P1-3: nil address panics (see the swap path above
        // for the placement rationale: after value evaluation, before
        // the storage-shape dispatch).
        requireNonNil(s);
        if (s instanceof AtomicReference<?> ar) {
            @SuppressWarnings("unchecked")
            AtomicReference<Object> cell = (AtomicReference<Object>) ar;
            while (true) {
                Object cur = cell.get();
                if (toHandle(cur) != oldVal) {
                    return false;
                }
                if (cell.compareAndSet(cur, Long.valueOf(newVal))) {
                    return true;
                }
            }
        }
        if (s instanceof GoPointer p) {
            synchronized (LOCK) {
                if (readPlain(p) != oldVal) {
                    return false;
                }
                p.setReferent(Long.valueOf(newVal));
                return true;
            }
        }
        throw new FrontendException("atomicptrop: address is neither a cell nor a pointer: "
                + s.getClass().getName());
    }

    /**
     * Builds the node from the {@code "atomicptrop"} wire node. Fields:
     * {@code op} ({@code "load"} | {@code "store"} | {@code "swap"} |
     * {@code "cas"}), {@code addr} (the {@code *unsafe.Pointer} address
     * expression), {@code args} (none for load; the new value for store
     * and swap; old and new for cas — all {@code unsafe.Pointer} handle
     * expressions), {@code type} (the result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx, NodeFactory factory) {
        Json.Object f = node.object("fields", "atomicptrop");
        String path = "atomicptrop.fields";
        String op = f.string("op", path);
        int wantArgs = switch (op) {
            case "load" -> 0;
            case "store" -> 1;
            case "swap" -> 1;
            case "cas" -> 2;
            default -> throw new FrontendException(path + ": unknown atomicptrop op " + op);
        };
        Json.Array arr = f.array("args", path);
        if (arr.size() != wantArgs) {
            throw new FrontendException(path + ": " + op + " takes " + wantArgs + " value arguments");
        }
        GoExprNode addr = factory.buildExpr(f.object("addr", path), ctx);
        GoExprNode[] args = new GoExprNode[arr.size()];
        for (int i = 0; i < arr.size(); i++) {
            Json.Value v = arr.get(i);
            if (!(v instanceof Json.Object o)) {
                throw new FrontendException(path + ".args[" + i + "]: must be a node object");
            }
            args[i] = factory.buildExpr(o, ctx);
        }
        return new AtomicPtrOpNode(op, addr, args);
    }
}
