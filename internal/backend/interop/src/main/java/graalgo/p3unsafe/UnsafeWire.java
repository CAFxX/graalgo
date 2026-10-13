package graalgo.p3unsafe;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * Shared wire-format helpers for the P3-M8 unsafe nodes.
 *
 * <p>Every unsafe wire node carries {@code "type"}: the result type-table
 * id (the ADR-0002 {@code syncnew} precedent — never guessed). The
 * {@code unsafeslice} node additionally reads {@code T}'s carrier from the
 * {@code []T} result entry's {@code "elem"} detail.
 */
final class UnsafeWire {
    private UnsafeWire() {}

    /** The numeric {@code "type"} field (result type-table id). */
    static int requireTypeId(Json.Object f, String kind) {
        String path = kind + ".fields";
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": " + kind + " needs a numeric \"type\" id");
        }
        return (int) num.asLong(path + ".type");
    }

    /** Builds the {@code "x"}/{@code "ptr"}/{@code "slice"}/{@code "s"} child. */
    static GoExprNode child(Json.Object f, String kind, String field,
            NodeFactory factory, NodeFactory.BuildCtx ctx) {
        String path = kind + ".fields";
        Json.Value v = f.opt(field);
        if (!(v instanceof Json.Object o)) {
            throw new FrontendException(path + ": " + kind + " needs \"" + field + "\"");
        }
        return factory.buildExpr(o, ctx);
    }

    /**
     * Resolves a type-table id to its {@link GoKind}, following
     * {@code named}→underlying and {@code alias}→rhs chains (mirrors
     * {@code NodeFactory.kindOf}'s chain walk; bounded).
     */
    static GoKind kindOfType(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > 32) {
                throw new FrontendException(path + ": type chain too deep");
            }
            String k = entry.string("kind", path);
            if ("named".equals(k)) {
                typeId = entry.object("detail", path).intValue("underlying", path);
                entry = ctx.types.entry(typeId, path);
            } else if ("alias".equals(k)) {
                typeId = entry.object("detail", path).intValue("rhs", path);
                entry = ctx.types.entry(typeId, path);
            } else {
                break;
            }
        }
        String k = entry.string("kind", path);
        if ("basic".equals(k)) {
            Json.Object detail = entry.object("detail", path);
            return GoKind.fromBasic(detail.string("name", path), detail.string("class", path));
        }
        return GoKind.fromCompositeKind(k);
    }

    /**
     * {@code T}'s carrier kind for an {@code unsafeslice} node whose
     * {@code "type"} is the {@code []T} result id.
     */
    static GoKind sliceElemKind(NodeFactory.BuildCtx ctx, int sliceTypeId, String path) {
        Json.Object entry = ctx.types.entry(sliceTypeId, path);
        if (!"slice".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": unsafeslice result must be a slice type");
        }
        int elemId = entry.object("detail", path).intValue("elem", path);
        return kindOfType(ctx, elemId, path);
    }
}
