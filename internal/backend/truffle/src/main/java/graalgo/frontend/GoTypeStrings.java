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
     *
     * <p>P3-M9 BUG-MODPROBE-011: named types render by display name
     * (package NAME form, e.g. "uuid.Version"), not the raw wire name
     * ("github.com/google/uuid.Version"). The method-table install keys,
     * the factory's runtime descriptors, and gc's own %T/panic rendering
     * all use the display form; the raw wire name leaked here only for
     * disk-imported types (for local types there is no slash, so
     * displayNameFor is the identity — which is why local types worked).
     */
    public static String of(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "types[" + typeId + "]";
        Json.Object entry = ctx.types.entry(typeId, path);
        String kind = entry.string("kind", path);
        Json.Object detail = entry.object("detail", path);
        String dpath = path + ".detail";
        return switch (kind) {
            case "basic" -> detail.string("name", dpath);
            case "named" -> NodeFactory.displayNameFor(detail.string("name", dpath));
            // The `any` alias renders as `interface {}` in gc messages.
            case "alias" -> "any".equals(detail.string("name", dpath))
                    ? "interface {}" : detail.string("name", dpath);
            // Pointer-to-named keeps the raw qualified name ("*"+qname):
            // the factory's runtime descriptors and the method-table
            // install keys both use "*"+qualifiedName for pointer-to-named
            // (dbd728c), so the lookup key must match byte-exactly.
            case "pointer" -> "*" + rawOf(ctx, detail.intValue("elem", dpath));
            case "slice" -> "[]" + of(ctx, detail.intValue("elem", dpath));
            case "array" -> "[" + detail.longInt("len", dpath) + "]"
                    + of(ctx, detail.intValue("elem", dpath));
            case "map" -> "map[" + of(ctx, detail.intValue("key", dpath)) + "]"
                    + of(ctx, detail.intValue("elem", dpath));
            case "chan" -> "chan " + of(ctx, detail.intValue("elem", dpath));
            case "interface" -> detail.bool("empty", dpath) ? "interface {}" : "interface{...}";
            // P0-fmt-022: anonymous structs render structurally, exactly as
            // gc does (`struct { A int; B string "tag" }`), not as the
            // `struct{...}` placeholder. Embedded fields render as just the
            // type name; tags are Go-quoted. This feeds %#v and %T (and
            // gc-exact panic messages).
            case "struct" -> structName(ctx, detail, dpath, false);
            default -> kind;
        };
    }

    /**
     * Go-syntax name for an anonymous struct type from its wire fields.
     * Each field renders as {@code Name Type} (or just {@code Type} when
     * embedded), with a Go-quoted tag when present; fields join with
     * {@code "; "}. Matches gc's reflect type strings verbatim.
     *
     * @param raw when true, field types render with {@link #rawOf} (the
     *            method-table keyspace); otherwise with {@link #of}.
     */
    private static String structName(NodeFactory.BuildCtx ctx, Json.Object detail, String dpath, boolean raw) {
        Json.Array fields = detail.array("fields", dpath);
        if (fields.size() == 0) {
            return "struct {}";
        }
        StringBuilder sb = new StringBuilder("struct { ");
        for (int i = 0; i < fields.size(); i++) {
            if (i > 0) {
                sb.append("; ");
            }
            String fpath = dpath + ".fields[" + i + "]";
            Json.Object f = (Json.Object) fields.get(i);
            int ftype = f.intValue("type", fpath);
            String typeName = raw ? rawOf(ctx, ftype) : of(ctx, ftype);
            boolean embedded = f.opt("embedded") instanceof Json.Bool b && b.value();
            if (!embedded) {
                sb.append(f.string("name", fpath)).append(' ');
            }
            sb.append(typeName);
            if (f.opt("tag") instanceof Json.Str tag && !tag.value().isEmpty()) {
                sb.append(' ').append(graalgo.runtime.GoQuote.quote(
                        tag.value().getBytes(java.nio.charset.StandardCharsets.UTF_8)));
            }
        }
        return sb.append(" }").toString();
    }

    /**
     * Pre-BUG-MODPROBE-011 rendering: raw wire names throughout, including
     * for named types. Used only for pointer element names, where the
     * method-table/install keyspace uses "*"+qualifiedName (full path)
     * for pointer-to-named — the display form would miss the install key.
     */
    private static String rawOf(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "types[" + typeId + "]";
        Json.Object entry = ctx.types.entry(typeId, path);
        String kind = entry.string("kind", path);
        Json.Object detail = entry.object("detail", path);
        String dpath = path + ".detail";
        return switch (kind) {
            case "basic", "named" -> detail.string("name", dpath);
            case "alias" -> "any".equals(detail.string("name", dpath))
                    ? "interface {}" : detail.string("name", dpath);
            case "pointer" -> "*" + rawOf(ctx, detail.intValue("elem", dpath));
            case "slice" -> "[]" + rawOf(ctx, detail.intValue("elem", dpath));
            case "array" -> "[" + detail.longInt("len", dpath) + "]"
                    + rawOf(ctx, detail.intValue("elem", dpath));
            case "map" -> "map[" + rawOf(ctx, detail.intValue("key", dpath)) + "]"
                    + rawOf(ctx, detail.intValue("elem", dpath));
            case "chan" -> "chan " + rawOf(ctx, detail.intValue("elem", dpath));
            case "interface" -> detail.bool("empty", dpath) ? "interface {}" : "interface{...}";
            // P0-fmt-022: same structural rendering as of(), so the
            // method-table keyspace stays consistent between the two.
            case "struct" -> structName(ctx, detail, dpath, true);
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
