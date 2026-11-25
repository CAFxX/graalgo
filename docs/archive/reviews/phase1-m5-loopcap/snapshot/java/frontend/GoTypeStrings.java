package graalgo.frontend;

/**
 * Go-syntax type names from the wire type table (M5, WS F).
 *
 * <p>Used for gc-exact panic messages (interface conversion, composite
 * panic rendering, named-type rendering). Unlike
 * {@link graalgo.values.GoTypeDescriptor#name()}, which is the canonical
 * kind name ({@code "int64"}), this renders the Go-syntax name from the
 * exporter's table ({@code "int"}, {@code "main.MyInt"}, {@code "[]int"}).
 */
public final class GoTypeStrings {
    private GoTypeStrings() {}

    /**
     * Go-syntax name for a wire type id. Handles basic, named, alias,
     * pointer, slice, array, map, chan, interface, and struct kinds; anything
     * else yields a best-effort placeholder (signatures are WS D).
     */
    public static String of(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "types[" + typeId + "]";
        Json.Object entry = ctx.types.entry(typeId, path);
        String kind = entry.string("kind", path);
        Json.Object detail = entry.object("detail", path);
        String dpath = path + ".detail";
        return switch (kind) {
            case "basic", "named" -> detail.string("name", dpath);
            // The `any` alias renders as `interface {}` in gc messages.
            case "alias" -> "any".equals(detail.string("name", dpath))
                    ? "interface {}" : detail.string("name", dpath);
            case "pointer" -> "*" + of(ctx, detail.intValue("elem", dpath));
            case "slice" -> "[]" + of(ctx, detail.intValue("elem", dpath));
            case "array" -> "[" + detail.longInt("len", dpath) + "]"
                    + of(ctx, detail.intValue("elem", dpath));
            case "map" -> "map[" + of(ctx, detail.intValue("key", dpath)) + "]"
                    + of(ctx, detail.intValue("elem", dpath));
            case "chan" -> "chan " + of(ctx, detail.intValue("elem", dpath));
            case "interface" -> detail.bool("empty", dpath) ? "interface {}" : "interface{...}";
            case "struct" -> "struct{...}";
            default -> kind;
        };
    }

    /**
     * True if the type id denotes a named basic type (a defined type whose
     * underlying is a predeclared basic kind), e.g. {@code main.MyInt}.
     */
    public static boolean isNamedBasic(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "types[" + typeId + "]";
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"named".equals(entry.string("kind", path))) {
            return false;
        }
        Json.Object detail = entry.object("detail", path);
        int under = detail.intValue("underlying", path + ".detail");
        return "basic".equals(ctx.types.entry(under, path).string("kind", path));
    }
}
