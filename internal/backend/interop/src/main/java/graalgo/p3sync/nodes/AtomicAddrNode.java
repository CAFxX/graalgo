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
            default -> throw new FrontendException("atomicaddr: unknown cell kind " + cell);
        };
    }

    /**
     * Builds the node from the {@code "atomicaddr"} wire node. Fields:
     * {@code addr} (the {@code *T} address expression) and
     * {@code cell} ({@code "int64"}, {@code "int32"}, or {@code "bool"}).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "atomicaddr");
        String path = "atomicaddr.fields";
        String cell = f.string("cell", path);
        if (!cell.equals("int64") && !cell.equals("int32")) {
            throw new FrontendException(path + ": unknown cell kind " + cell);
        }
        GoExprNode addr = factory.buildExpr(f.object("addr", path), ctx);
        return new AtomicAddrNode(addr, cell);
    }
}
