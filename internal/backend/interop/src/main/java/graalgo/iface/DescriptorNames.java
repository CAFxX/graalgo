package graalgo.iface;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.TypeTable;
import graalgo.values.GoKind;

/**
 * Replica of {@code NodeFactory}'s descriptor-naming branch dispatch
 * ({@code typeDescriptorFor} / {@code kindOf} / {@code resolveAlias} /
 * {@code structuralKey}), for use from the {@code graalgo.iface} package,
 * which may not touch Phase-1 privates (frozen).
 *
 * <p>Only the branch <em>dispatch</em> is replicated, never the canonical
 * instances: the dispatch path keys itabs by descriptor name, and the
 * launcher must compute the same name strings the factory mints at box
 * sites so exporter and runtime agree without a shared-code change
 * (ADR-0003 D5). Leaf naming ({@code displayNameFor},
 * {@code goSyntaxName}, {@code defaultBasicName}) delegates to the
 * factory's public methods — a single source of truth, so the
 * dbd728c/fa099f2 drift (replica silently stale while the factory moved
 * on, breaking method-table keys) cannot recur. Where the factory returns
 * a canonical instance ({@code basicDescriptorForName},
 * {@code ctx.typeDescs}), callers use the public routes; this class
 * covers only the name computation.
 *
 * <p>Deviation note (ADR-0003 D5, updated P3-M7R2): the ADR claims pointer
 * dynamic types key by {@code "pointer#"+typeId}. The factory
 * ({@code NodeFactory}, {@code typeDescriptorFor}) routes pointer-to-named
 * types to {@code "*"+qualifiedName} (dbd728c) and other pointers through
 * the structural branch, naming them via {@code goSyntaxName} (fa099f2)
 * with a {@code "pointer#" + hex(structuralKey.hashCode())} fallback —
 * the {@code k + "#" + typeId} branch the ADR cites is unreachable for
 * pointers. Named types use the display name ({@code displayNameFor}),
 * not the qualified name (fa099f2). This class replicates the factory's
 * <em>actual</em> branch dispatch; leaf naming ({@code displayNameFor},
 * {@code goSyntaxName}) delegates to the factory as a single source of
 * truth. ADR-0003 itself is not modified here (frozen doc); the deviation
 * is reported to the coordinator for an ADR amendment.
 *
 * <p>Correctness of the replica is pinned by
 * {@code graalgo.frontend.DescriptorNamesTest}, which compares these
 * names against the real factory's {@code typeDescriptorFor} across a
 * battery of type-table shapes.
 */
public final class DescriptorNames {
    private DescriptorNames() {}

    /** Bound on type-chain walks (mirrors {@code NodeFactory}'s private bound). */
    private static final int MAX_TYPE_CHAIN_STEPS = 10_000;

    /**
     * The descriptor name {@code NodeFactory.typeDescriptorFor} would mint
     * for {@code typeId} (basic/named/interface/structural/other branches).
     *
     * <p>P3-M7R2: the branch dispatch is replicated independently (this is
     * what the test pins), but leaf naming delegates to the factory's
     * {@code displayNameFor}/{@code goSyntaxName} — a single source of
     * truth, so the dbd728c/fa099f2 drift (replica silently stale while
     * the factory moved on) cannot recur.
     */
    public static String descriptorName(TypeTable types, int typeId) {
        String path = "types[" + typeId + "]";
        typeId = resolveAlias(types, typeId, path);
        Json.Object entry = types.entry(typeId, path);
        String k = entry.string("kind", path);
        if ("basic".equals(k)) {
            // Factory (P3-M7, P3-M7R2): basic types canonicalize by Go
            // name via basicDescriptorForName ("int" vs "int64" are
            // distinct); untyped constants normalize to default names
            // and alias spellings ("rune"/"byte") to canonical names —
            // single source of truth in NodeFactory.canonicalBasicName.
            String name = entry.object("detail", path).string("name", path);
            return graalgo.frontend.NodeFactory.canonicalBasicName(name);
        }
        Json.Object detail = entry.object("detail", path);
        if ("pointer".equals(k)) {
            // Factory (dbd728c): pointer-to-named is "*"+qualifiedName,
            // not structural. Pointer-to-unnamed falls through to the
            // structural branch below.
            int elemId = resolveAlias(types, detail.intValue("elem", path), path);
            Json.Object elemEntry = types.entry(elemId, path);
            if ("named".equals(elemEntry.string("kind", path))) {
                return "*" + elemEntry.object("detail", path).string("name", path);
            }
        }
        if (isStructuralComposite(k)) {
            // Factory (fa099f2): structural descriptors use goSyntaxName
            // for %T, falling back to k#hash.
            try {
                return graalgo.frontend.NodeFactory.goSyntaxName(types, typeId, path);
            } catch (FrontendException e) {
                String key = structuralKey(types, typeId, path);
                return k + "#" + Integer.toHexString(key.hashCode());
            }
        }
        if ("named".equals(k)) {
            return graalgo.frontend.NodeFactory.displayNameFor(detail.string("name", path));
        }
        if ("interface".equals(k)) {
            boolean empty = detail.bool("empty", path);
            return empty ? "any" : "interface#" + typeId;
        }
        return k + "#" + typeId;
    }

    /**
     * The interface-method-table key for a type-table entry (N-P1-2):
     * the qualified name for named types (detail {@code name} verbatim,
     * e.g. {@code "a/foo.T"}) and for pointer-to-named types
     * ({@code "*"} + the element's qualified name), and
     * {@link #descriptorName} for everything else (structural types,
     * whose key already encodes the full shape, and basic/interface
     * entries, which carry no methods). This is the install-side
     * counterpart of {@link graalgo.values.GoTypeDescriptor#methodTableKey}:
     * the two must agree byte-exactly, or dispatch misses its table.
     * The display name is deliberately NOT used here — it is a %T
     * form, not an identity, and collides across packages sharing a
     * package name. Interface-name lists ({@code resolveInterfaces})
     * are a separate namespace and keep display-name keying (their
     * lookup side uses {@code iface.name()}).
     */
    public static String methodTableKey(TypeTable types, int typeId) {
        String path = "types[" + typeId + "]";
        typeId = resolveAlias(types, typeId, path);
        Json.Object entry = types.entry(typeId, path);
        String k = entry.string("kind", path);
        Json.Object detail = entry.object("detail", path);
        if ("named".equals(k)) {
            return detail.string("name", path);
        }
        if ("pointer".equals(k)) {
            int elemId = resolveAlias(types, detail.intValue("elem", path), path);
            Json.Object elemEntry = types.entry(elemId, path);
            if ("named".equals(elemEntry.string("kind", path))) {
                return "*" + elemEntry.object("detail", path).string("name", path);
            }
        }
        return descriptorName(types, typeId);
    }

    /**
     * Replica of {@code NodeFactory.kindOf}: named resolves to underlying,
     * alias to its rhs (bounded walk, fail-fast on cycles).
     */
    public static GoKind kindOf(TypeTable types, int typeId, String path) {
        Json.Object entry = types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            String ek = entry.string("kind", path);
            if ("named".equals(ek)) {
                typeId = entry.object("detail", path).intValue("underlying", path);
                entry = types.entry(typeId, path);
            } else if ("alias".equals(ek)) {
                typeId = entry.object("detail", path).intValue("rhs", path);
                entry = types.entry(typeId, path);
            } else {
                break;
            }
        }
        String k = entry.string("kind", path);
        try {
            if ("basic".equals(k)) {
                Json.Object detail = entry.object("detail", path);
                return GoKind.fromBasic(detail.string("name", path),
                        detail.string("class", path));
            }
            return GoKind.fromCompositeKind(k);
        } catch (IllegalArgumentException e) {
            throw new FrontendException(path + ": " + e.getMessage()
                    + " (WS C/D hook: NodeFactory.registerTypeDecoder)");
        }
    }

    /** Replica of {@code NodeFactory.resolveAlias} (alias-only steps). */
    public static int resolveAlias(TypeTable types, int typeId, String path) {
        Json.Object entry = types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": alias chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            if (!"alias".equals(entry.string("kind", path))) {
                return typeId;
            }
            typeId = entry.object("detail", path).intValue("rhs", path);
            entry = types.entry(typeId, path);
        }
    }

    private static boolean isStructuralComposite(String k) {
        // Mirrors NodeFactory.isStructuralComposite (P3-M7: signature and
        // chan are structural).
        return "struct".equals(k) || "array".equals(k) || "slice".equals(k)
                || "map".equals(k) || "pointer".equals(k) || "signature".equals(k)
                || "chan".equals(k);
    }

    /** Replica of {@code NodeFactory.structuralKey}. */
    private static String structuralKey(TypeTable types, int typeId, String path) {
        typeId = resolveAlias(types, typeId, path);
        Json.Object entry = types.entry(typeId, path);
        String k = entry.string("kind", path);
        Json.Object detail = entry.object("detail", path);
        switch (k) {
            case "basic":
                return "basic:" + kindOf(types, typeId, path).name();
            case "named":
                return "named(" + detail.string("name", path) + ")";
            case "struct": {
                StringBuilder sb = new StringBuilder("struct{");
                Json.Array fields = detail.array("fields", path);
                for (int i = 0; i < fields.size(); i++) {
                    Json.Value fv = fields.get(i);
                    if (!(fv instanceof Json.Object fo)) {
                        throw new FrontendException(path + ": struct field not an object");
                    }
                    sb.append(fo.string("name", path)).append(':')
                            .append(structuralKey(types, fo.intValue("type", path), path))
                            .append(';');
                }
                return sb.append('}').toString();
            }
            case "array":
                return "array[" + detail.intValue("len", path) + "]"
                        + structuralKey(types, detail.intValue("elem", path), path);
            case "slice":
                return "slice[" + structuralKey(types, detail.intValue("elem", path), path) + "]";
            case "map":
                return "map[" + structuralKey(types, detail.intValue("key", path), path) + "]"
                        + structuralKey(types, detail.intValue("elem", path), path);
            case "pointer":
                return "ptr(" + structuralKey(types, detail.intValue("elem", path), path) + ")";
            case "chan":
                // P3-M7: chan types are structural.
                return "chan(" + structuralKey(types, detail.intValue("elem", path), path) + ")";
            case "signature": {
                // P3-M7: func types are structural.
                StringBuilder sb = new StringBuilder("func(");
                Json.Array params = detail.array("params", path);
                for (int i = 0; i < params.size(); i++) {
                    Json.Object p = (Json.Object) params.get(i);
                    sb.append(structuralKey(types, p.intValue("type", path), path)).append(',');
                }
                sb.append(")(");
                Json.Array results = detail.array("results", path);
                for (int i = 0; i < results.size(); i++) {
                    Json.Object r = (Json.Object) results.get(i);
                    sb.append(structuralKey(types, r.intValue("type", path), path)).append(',');
                }
                sb.append(')');
                if (detail.opt("variadic") instanceof Json.Bool b && b.value()) {
                    sb.append("variadic");
                }
                return sb.toString();
            }
            default:
                // interface, invalid: program-local identity.
                return k + "#" + typeId;
        }
    }
}
