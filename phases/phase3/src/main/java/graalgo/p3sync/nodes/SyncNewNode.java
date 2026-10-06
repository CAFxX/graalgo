package graalgo.p3sync.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.sync.GoMutex;
import graalgo.runtime.sync.GoOnce;
import graalgo.runtime.sync.GoRWMutex;
import graalgo.runtime.sync.GoWaitGroup;
import graalgo.values.GoKind;

/**
 * Construction of a {@code sync} primitive (P3-M2b, WS D, ADR-0002) or a
 * {@code sync/atomic} wrapper cell (P3-M4, WS C, ADR-0004 D2): the
 * {@code "syncnew"} wire node.
 *
 * <p>A {@code sync.Mutex} Go value <em>is</em> the {@link GoMutex} Java
 * object (opaque, like {@code GoChan} — ADR-0002 D1); an
 * {@code atomic.Int64} Go value <em>is</em> the {@link AtomicLong} cell,
 * an {@code atomic.Int32}/{@code Uint32}/{@code Bool} value <em>is</em>
 * the {@link AtomicInteger} cell. Copying copies the reference:
 * {@code Boxing.copyForAssign} deep-copies only {@code GoStruct} values,
 * and these objects are not {@code GoStruct}s, so assignment, argument
 * passing, and interface boxing all carry the reference with no extra
 * machinery.
 *
 * <p>The node kind is {@link GoKind#STRUCT} for the value form
 * ({@code sync.Mutex{}}, {@code atomic.Int64{}}) and {@link GoKind#POINTER}
 * for the pointer form ({@code &sync.Mutex{}}, {@code new(sync.Mutex)}),
 * read from the wire type-table entry — never guessed.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook (P2-D8 precedent) — no
 * Phase-1 {@code NodeFactory} change.
 *
 * <p>verifies: GG-0017 (sync slice); verifies: GG-0020 (atomic slice)
 */
public final class SyncNewNode extends GoExprNode {

    private final String syncType;

    private SyncNewNode(GoKind kind, String syncType) {
        super(kind);
        this.syncType = syncType;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return switch (syncType) {
            case "Mutex" -> new GoMutex();
            case "RWMutex" -> new GoRWMutex();
            case "WaitGroup" -> new GoWaitGroup();
            case "Once" -> new GoOnce();
            // P3-M4 (WS C, ADR-0004 D2): atomic wrapper cells. 64-bit
            // wrappers ride on AtomicLong, 32-bit/bool on AtomicInteger
            // (the phase-1 Atomics backing).
            case "atomic.Int64", "atomic.Uint64", "atomic.Uintptr" -> new AtomicLong();
            case "atomic.Int32", "atomic.Uint32", "atomic.Bool" -> new AtomicInteger();
            default -> throw new IllegalStateException("unknown sync type: " + syncType);
        };
    }

    /**
     * Builds the node from the {@code "syncnew"} wire node. Fields:
     * {@code synctyp} (the primitive name), {@code type} (type-table id —
     * the named sync type, or a pointer to it for the {@code &}/{@code new}
     * forms).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "syncnew");
        String path = "syncnew.fields";
        String syncType = f.string("synctyp", path);
        switch (syncType) {
            case "Mutex", "RWMutex", "WaitGroup", "Once",
                 // P3-M4 (WS C, ADR-0004 D2): atomic wrapper cells.
                 "atomic.Int64", "atomic.Uint64", "atomic.Int32",
                 "atomic.Uint32", "atomic.Bool", "atomic.Uintptr" -> { /* ok */ }
            default -> throw new FrontendException(path + ": unknown sync type " + syncType);
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": syncnew needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        return new SyncNewNode(kindForType(ctx, typeId, path), syncType);
    }

    /**
     * The GoKind for a {@code syncnew} result type: the named sync/atomic
     * type lowers to its {@code struct{}} underlying ({@link GoKind#STRUCT});
     * a pointer to it is {@link GoKind#POINTER}. Anything else is a broken
     * exporter, failed fast — never guessed.
     */
    private static GoKind kindForType(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        String kind = entry.string("kind", path);
        switch (kind) {
            case "named" -> {
                String name = entry.object("detail", path).string("name", path);
                // P3-M4: the atomic overlay's synthetic package path is
                // "sync/atomic", so its names read "sync/atomic.Int64".
                if (!name.startsWith("sync.") && !name.startsWith("sync/atomic.")) {
                    throw new FrontendException(
                            path + ": syncnew of non-sync named type " + name);
                }
                return GoKind.STRUCT;
            }
            case "pointer" -> {
                return GoKind.POINTER;
            }
            default -> throw new FrontendException(
                    path + ": syncnew of unsupported type kind " + kind);
        }
    }
}
