package graalgo.p3sync.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.runtime.sync.GoPointerCell32;
import graalgo.runtime.sync.GoPointerCell64;
import graalgo.runtime.sync.IntCell;
import graalgo.runtime.sync.LongCell;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * An atomic cell view over a Go address (fix-linkname, Caddy/godebug
 * lane): the {@code "atomicaddr"} wire node.
 *
 * <p>Package-level {@code sync/atomic} operations
 * ({@code atomic.LoadUint64(&x)}, …) take a {@code *T} address operand.
 * This node evaluates the address to its {@link GoPointer} and wraps it
 * in a delegating {@link GoPointerCell64} / {@link GoPointerCell32},
 * so the existing {@code "syncop"} atomic machinery
 * ({@link AtomicOpNode}) serves the operation unchanged. A nil address
 * is gc's nil-dereference panic.
 *
 * <p>P1-F2 (review3-frontend): the {@code "ptr64"} cell kind serves the
 * package-level pointer ops ({@code atomic.LoadPointer} /
 * {@code atomic.StorePointer}). A Go {@code unsafe.Pointer} value is a
 * long handle into the unsafe handle table (D5), so the 64-bit
 * lock-serialized delegating cell is exactly the right view — the same
 * class and shared-lock domain as the word ops, preserving the handle
 * representation (unlike the typed {@code atomic.Pointer[T]} wrappers,
 * whose cells hold opaque references).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1
 * {@code NodeFactory} change.
 */
public final class AtomicAddrNode extends GoExprNode {

    @Child
    private GoExprNode addr;
    private final String cell;

    private AtomicAddrNode(GoExprNode addr, String cell) {
        // The node evaluates to the cell object itself (opaque, like the
        // atomic wrapper cells); the kind is never directly observed.
        super(GoKind.INTERFACE);
        this.addr = addr;
        this.cell = cell;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object a = addr.executeObject(frame);
        // PKG-053: &x.v on an atomic wrapper cell evaluates to the cell
        // itself (FieldAddrNode) — it already IS the IntCell / LongCell /
        // AtomicReference the syncop machinery needs, so use it directly
        // instead of wrapping a pointer.
        if (a instanceof IntCell || a instanceof LongCell
                || a instanceof java.util.concurrent.atomic.AtomicReference<?>) {
            return a;
        }
        GoPointer gp;
        if (a instanceof GoPointer q && !q.isNil()) {
            gp = q;
        } else {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            throw new AssertionError("unreachable");
        }
        return switch (cell) {
            case "int64" -> new GoPointerCell64(gp);
            case "int32" -> new GoPointerCell32(gp);
            // P1-F2 (review3-frontend): unsafe.Pointer handles are longs;
            // the pointer ops share the 64-bit lock-serialized cell.
            case "ptr64" -> new GoPointerCell64(gp);
            default -> throw new FrontendException("atomicaddr: unknown cell kind " + cell);
        };
    }

    /**
     * Builds the node from the {@code "atomicaddr"} wire node. Fields:
     * {@code addr} (the {@code *T} address expression) and
     * {@code cell} ({@code "int64"}, {@code "int32"}, or {@code "ptr64"}
     * for the unsafe.Pointer handle cell — P1-F2).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "atomicaddr");
        String path = "atomicaddr.fields";
        String cell = f.string("cell", path);
        if (!cell.equals("int64") && !cell.equals("int32") && !cell.equals("ptr64")) {
            throw new FrontendException(path + ": unknown cell kind " + cell);
        }
        GoExprNode addr = factory.buildExpr(f.object("addr", path), ctx);
        return new AtomicAddrNode(addr, cell);
    }
}
