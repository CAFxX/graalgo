package graalgo.p3sync.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

import java.util.concurrent.atomic.AtomicReference;

/**
 * Package-level {@code sync/atomic} pointer read-modify-write
 * (fix-redmaster-r39): the {@code "atomicptrop"} wire node, lowering
 * {@code atomic.SwapPointer} ({@code op="swap"}, result the old
 * {@code unsafe.Pointer}) and {@code atomic.CompareAndSwapPointer}
 * ({@code op="cas"}, result bool). The frontend emits it for both the
 * qualified forms and the unqualified calls inside {@code sync/atomic}
 * itself (the {@code atomic.Pointer[T]} method bodies in type.go call
 * the bodyless assembly declarations, which previously reached the
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
 *   see {@code FieldAddrNode}): the swap/CAS loop runs on the cell with
 *   real atomicity. A cell content written by another path as a
 *   {@link GoPointer} is normalized to its handle for comparison and
 *   replaced in handle form on write.</li>
 *   <li>a plain {@link GoPointer} to an {@code unsafe.Pointer} field:
 *   operations serialize on a shared lock (the {@code GoPointerCell64}
 *   scoped-divergence precedent: mutually atomic RMW, no lock-free
 *   progress or ordering guarantees beyond that).</li>
 * </ul>
 *
 * <p>Handle equality is pointer identity (PKG-007): two handles are
 * equal iff they name the same storage, so a handle-valued CAS matches
 * gc's address CAS on every representable case.
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

    /** The addressed storage, unwrapped to a cell or a plain pointer. */
    private Object storage(VirtualFrame frame) {
        Object a = addrChild.executeObject(frame);
        if (a == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            throw new AssertionError("unreachable");
        }
        return a;
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
        throw new FrontendException("atomicptrop: plain field holds a non-handle value "
                + r.getClass().getName());
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // swap: returns the old handle.
        Object s = storage(frame);
        long newVal = argChildren[0].executeLong(frame);
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

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        // cas: compares the stored handle against old, stores new on match.
        Object s = storage(frame);
        long oldVal = argChildren[0].executeLong(frame);
        long newVal = argChildren[1].executeLong(frame);
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
     * {@code op} ({@code "swap"} | {@code "cas"}), {@code addr} (the
     * {@code *unsafe.Pointer} address expression), {@code args} (the new
     * value for swap; old and new for cas — all {@code unsafe.Pointer}
     * handle expressions), {@code type} (the result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx, NodeFactory factory) {
        Json.Object f = node.object("fields", "atomicptrop");
        String path = "atomicptrop.fields";
        String op = f.string("op", path);
        int wantArgs = switch (op) {
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
