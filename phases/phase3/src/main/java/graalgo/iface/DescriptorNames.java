package graalgo.iface;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.TypeTable;
import graalgo.values.GoKind;

/**
 * Byte-exact replica of {@code NodeFactory}'s private descriptor-naming
 * logic ({@code typeDescriptorFor} / {@code kindOf} / {@code resolveAlias} /
 * {@code structuralKey}), for use from the {@code graalgo.iface} package,
 * which may not touch Phase-1 privates (frozen).
 *
 * <p>Only the <em>names</em> are replicated, never the canonical
 * instances: the dispatch path keys itabs by descriptor name, and the
 * launcher must compute the same name strings the factory mints at box
 * sites so exporter and runtime agree without a shared-code change
 * (ADR-0003 D5). Where the factory returns a canonical instance
 * ({@code basicDescriptorFor}, {@code ctx.typeDescs}), callers use the
 * public routes; this class covers only the name computation.
 *
 * <p>Deviation note (ADR-0003 D5): the ADR claims pointer dynamic types
 * key by {@code "pointer#"+typeId}. The factory ({@code NodeFactory},
 * {@code typeDescriptorFor}) in fact routes every {@code pointer} entry
 * through the structural branch, naming it
 * {@code "pointer#" + hex(structuralKey.hashCode())} — the
 * {@code k + "#" + typeId} branch the ADR cites is unreachable for
 * pointers. This class replicates the factory's <em>actual</em> naming;
 * using the ADR's literal string would never match a runtime descriptor
 * and every pointer-receiver interface call would fail table lookup.
 * ADR-0003 itself is not modified here (frozen doc); the deviation is
 * reported to the coordinator for an ADR amendment.
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
     */
    public static String descriptorName(TypeTable types, int typeId) {
        String path = "types[" + typeId + "]";
        typeId = resolveAlias(types, typeId, path);
        Json.Object entry = types.entry(typeId, path);
        String k = entry.string("kind", path);
        if ("basic".equals(k)) {
            // Factory: basicDescriptorFor(kindOf(...)) names by kind.
            return kindOf(types, typeId, path).name().toLowerCase();
        }
        if (isStructuralComposite(k)) {
            String key = structuralKey(types, typeId, path);
            return k + "#" + Integer.toHexString(key.hashCode());
        }
        Json.Object detail = entry.object("detail", path);
        if ("named".equals(k)) {
            return detail.string("name", path);
        }
        if ("interface".equals(k)) {
            boolean empty = detail.bool("empty", path);
            return empty ? "any" : "interface#" + typeId;
        }
        return k + "#" + typeId;
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
        return "struct".equals(k) || "array".equals(k) || "slice".equals(k)
                || "map".equals(k) || "pointer".equals(k);
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
            default:
                // interface, signature, invalid: program-local identity.
                return k + "#" + typeId;
        }
    }
}
