package graalgo.p3sync.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

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
 * Construction of a {@code sync} primitive (P3-M2b, WS D, ADR-0002):
 * the {@code "syncnew"} wire node.
 *
 * <p>A {@code sync.Mutex} Go value <em>is</em> the {@link GoMutex} Java
 * object (opaque, like {@code GoChan} — ADR-0002 D1). Copying copies the
 * reference: {@code Boxing.copyForAssign} deep-copies only
 * {@code GoStruct} values, and these objects are not {@code GoStruct}s,
 * so assignment, argument passing, and interface boxing all carry the
 * reference with no extra machinery.
 *
 * <p>The node kind is {@link GoKind#STRUCT} for the value form
 * ({@code sync.Mutex{}}) and {@link GoKind#POINTER} for the pointer form
 * ({@code &sync.Mutex{}}, {@code new(sync.Mutex)}), read from the wire
 * type-table entry — never guessed.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook (P2-D8 precedent) — no
 * Phase-1 {@code NodeFactory} change.
 *
 * <p>verifies: GG-0017 (sync slice)
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
            case "Mutex", "RWMutex", "WaitGroup", "Once" -> { /* ok */ }
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
     * The GoKind for a {@code syncnew} result type: the named sync type
     * lowers to its {@code struct{}} underlying ({@link GoKind#STRUCT}); a
     * pointer to it is {@link GoKind#POINTER}. Anything else is a broken
     * exporter, failed fast — never guessed.
     */
    private static GoKind kindForType(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        String kind = entry.string("kind", path);
        switch (kind) {
            case "named" -> {
                String name = entry.object("detail", path).string("name", path);
                if (!name.startsWith("sync.")) {
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
