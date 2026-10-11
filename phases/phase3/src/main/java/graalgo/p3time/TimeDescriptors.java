package graalgo.p3time;

import java.util.List;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * Resolution of the synthetic {@code time.Time} struct descriptor (P3-M5,
 * WS E, ADR-0005 D1): {@code struct{ unixnano int64; isSet bool }}.
 *
 * <p>The descriptor must be the <em>canonical</em> one — the same
 * instance {@code NodeFactory.structDescriptorFor} would mint — because
 * descriptor identity is Go struct type identity (map keys and struct
 * equality hash by it). The factory's method is package-private, so this
 * replicates its exact behavior against the public
 * {@link NodeFactory.BuildCtx} caches: resolve the {@code "time.Time"}
 * named entry to its underlying struct, validate the {@code unixnano
 * int64} + {@code isSet bool} fields (fail fast — the exporter contract),
 * and populate {@code ctx.structDescs} / {@code ctx.structFieldTypeIds}
 * with the {@code "struct#"+id} instance the factory would have built. A
 * later factory-built Time value then shares this instance, and vice
 * versa.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeDescriptors {
    private TimeDescriptors() {}

    /** Bound on the named-type chain walk (mirrors NodeFactory's). */
    private static final int MAX_TYPE_CHAIN_STEPS = 10_000;

    /**
     * The canonical {@link GoStructDescriptor} for the synthetic
     * {@code time.Time}. Scans the type table for the named type
     * {@code "time.Time"} (the synthetic package's path is
     * {@code "time"}), so builders need no ordering contract.
     */
    public static GoStructDescriptor resolve(NodeFactory.BuildCtx ctx, String path) {
        int namedId = -1;
        for (int i = 0; i < ctx.types.size(); i++) {
            Json.Object e = ctx.types.entry(i, path);
            if (!"named".equals(e.string("kind", path))) {
                continue;
            }
            if ("time.Time".equals(e.object("detail", path).string("name", path))) {
                namedId = i;
                break;
            }
        }
        if (namedId < 0) {
            throw new FrontendException(
                    path + ": time.Time not found in type table (is \"time\" imported?)");
        }
        // Resolve named -> underlying, mirroring NodeFactory.structDescriptorFor.
        int typeId = namedId;
        Json.Object entry = ctx.types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": named-type chain too long");
            }
            if (!"named".equals(entry.string("kind", path))) {
                break;
            }
            typeId = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(typeId, path);
        }
        if (!"struct".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": time.Time underlying is not a struct");
        }
        Json.Array fields = entry.object("detail", path).array("fields", path);
        if (fields.size() != 2) {
            throw new FrontendException(
                    path + ": time.Time must be struct{ unixnano int64; isSet bool }, found "
                            + fields.size() + " fields");
        }
        Json.Value fv = fields.get(0);
        if (!(fv instanceof Json.Object fo)) {
            throw new FrontendException(path + ": time.Time field 0 is not an object");
        }
        if (!"unixnano".equals(fo.string("name", path))) {
            throw new FrontendException(
                    path + ": time.Time field 0 must be \"unixnano\", found \""
                            + fo.string("name", path) + "\"");
        }
        int fieldTypeId = fo.intValue("type", path);
        if (fieldKind(ctx, fieldTypeId, path) != GoKind.INT64) {
            throw new FrontendException(path + ": time.Time.unixnano must be int64");
        }
        Json.Value fv1 = fields.get(1);
        if (!(fv1 instanceof Json.Object fo1)) {
            throw new FrontendException(path + ": time.Time field 1 is not an object");
        }
        if (!"isSet".equals(fo1.string("name", path))) {
            throw new FrontendException(
                    path + ": time.Time field 1 must be \"isSet\", found \""
                            + fo1.string("name", path) + "\"");
        }
        int fieldTypeId1 = fo1.intValue("type", path);
        if (fieldKind(ctx, fieldTypeId1, path) != GoKind.BOOL) {
            throw new FrontendException(path + ": time.Time.isSet must be bool");
        }
        GoStructDescriptor d = ctx.structDescs.get(typeId);
        if (d == null) {
            d = GoStructDescriptor.layout("struct#" + typeId,
                    List.of("unixnano", "isSet"), List.of(GoKind.INT64, GoKind.BOOL));
            ctx.structDescs.put(typeId, d);
            ctx.structFieldTypeIds.put(typeId, new int[]{fieldTypeId, fieldTypeId1});
        }
        return d;
    }

    /** Field kind through aliases/named types (mirrors NodeFactory.kindOf for basics). */
    private static GoKind fieldKind(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": type chain too long");
            }
            String ek = entry.string("kind", path);
            if ("named".equals(ek)) {
                typeId = entry.object("detail", path).intValue("underlying", path);
                entry = ctx.types.entry(typeId, path);
            } else if ("alias".equals(ek)) {
                typeId = entry.object("detail", path).intValue("rhs", path);
                entry = ctx.types.entry(typeId, path);
            } else {
                break;
            }
        }
        if (!"basic".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": time.Time.unixnano must be a basic type");
        }
        Json.Object detail = entry.object("detail", path);
        try {
            return GoKind.fromBasic(detail.string("name", path), detail.string("class", path));
        } catch (IllegalArgumentException e) {
            throw new FrontendException(path + ": " + e.getMessage());
        }
    }
}
