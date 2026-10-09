package graalgo.p3chan.nodes;

import java.util.ArrayList;
import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.ConstNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.jfr.ChannelOp;
import graalgo.jfr.JfrEvents;
import graalgo.jfr.ParkReason;
import graalgo.runtime.ChanOps;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Shared wire-format and type helpers for the P3-M5 channel nodes
 * (ADR-0005 Appendix C): {@code "makechan"}, {@code "send"},
 * {@code "recv"}, {@code "chanclose"}, {@code "select"}.
 *
 * <p>All five node kinds are registered on the factory via the public
 * {@code registerExprKind}/{@code registerStmtKind} hooks — no Phase-1
 * or Phase-2 change. The Phase-2 primitives ({@link ChanOps},
 * {@code SelectOp}, {@code GoChan}) are consumed as-is.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
final class ChanWire {
    private ChanWire() {}

    /** Bound on type-chain walks (mirrors NodeFactory's M10 guard). */
    private static final int MAX_TYPE_CHAIN_STEPS = 10_000;

    /**
     * The numeric {@code "elem"} field (element type-table id) of
     * {@code "makechan"}.
     */
    static int requireElemId(Json.Object f, String path) {
        return f.intValue("elem", path);
    }

    /**
     * The optional {@code "size"} field of {@code "makechan"} as an
     * int-valued expression: a literal number (buffer capacity) or an
     * expression node. Null when absent (unbuffered).
     */
    static GoExprNode sizeNode(Json.Object f, NodeFactory factory,
                               NodeFactory.BuildCtx ctx, String path) {
        Json.Value v = f.opt("size");
        if (v == null) {
            return null;
        }
        GoExprNode n;
        if (v instanceof Json.Num num) {
            n = ConstNode.ofInt(GoKind.INT64, num.asLong(path + ".size"));
        } else if (v instanceof Json.Object o) {
            n = factory.buildExpr(o, ctx);
        } else {
            throw new FrontendException(path + ".size: must be a number or a node object");
        }
        if (!n.kind().isIntKind()) {
            throw new FrontendException(path + ".size: must be an int kind, got " + n.kind());
        }
        return n;
    }

    /** A required expression-node field. */
    static GoExprNode exprField(Json.Object f, String name, String kind,
                                NodeFactory factory, NodeFactory.BuildCtx ctx) {
        String path = kind + ".fields";
        Json.Value v = f.opt(name);
        if (!(v instanceof Json.Object o)) {
            throw new FrontendException(path + ": " + kind + " needs an expression node \"" + name + "\"");
        }
        return factory.buildExpr(o, ctx);
    }

    /**
     * Maps a type-table id to its {@link GoKind}, resolving named types to
     * their underlying type and aliases to their right-hand side.
     * Replicates {@code NodeFactory.kindOf} (package-private) against the
     * public {@link NodeFactory.BuildCtx} surface — the same replication
     * pattern as {@code TimeDescriptors} (P3-M5).
     */
    static GoKind kindOf(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
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
        String k = entry.string("kind", path);
        try {
            if ("basic".equals(k)) {
                Json.Object detail = entry.object("detail", path);
                return GoKind.fromBasic(detail.string("name", path), detail.string("class", path));
            }
            return GoKind.fromCompositeKind(k);
        } catch (IllegalArgumentException e) {
            throw new FrontendException(path + ": " + e.getMessage()
                    + " (WS C/D hook: NodeFactory.registerTypeDecoder)");
        }
    }

    /**
     * The type-table id of the {@code "struct"} entry underlying a
     * (possibly named/aliased) struct type id. Mirrors the chain walk in
     * {@code NodeFactory.structDescriptorFor}.
     */
    private static int structEntryId(NodeFactory.BuildCtx ctx, int typeId, String path) {
        typeId = resolveAliasOnly(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": named-type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?)");
            }
            if (!"named".equals(entry.string("kind", path))) {
                break;
            }
            typeId = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(typeId, path);
        }
        if (!"struct".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": not a struct type");
        }
        return typeId;
    }

    /**
     * The canonical {@link GoStructDescriptor} for a struct type id.
     *
     * <p>P3-M8b: delegates to the now-public
     * {@code NodeFactory.structDescriptorFor} (process-wide canonical
     * instances) instead of minting a per-BuildCtx replica — the replica
     * broke the "descriptor identity is type identity" contract across
     * functions, so channel zero values never equaled factory-built
     * values of the same struct type.
     */
    static GoStructDescriptor structDescriptorFor(NodeFactory.BuildCtx ctx, int typeId, String path) {
        return NodeFactory.structDescriptorFor(ctx, typeId, path);
    }

    /** One {@code "alias"}-only resolution step chain (aliases are their rhs). */
    private static int resolveAliasOnly(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": alias chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?)");
            }
            if (!"alias".equals(entry.string("kind", path))) {
                return typeId;
            }
            typeId = entry.object("detail", path).intValue("rhs", path);
            entry = ctx.types.entry(typeId, path);
        }
    }

    /**
     * The element zero value template for a channel element type id —
     * observed by recv on a closed, drained channel (Go's
     * {@code (zero, false)}).
     *
     * <p>Mirrors {@code NodeFactory.zeroNode}'s build-time templates:
     * scalar/string/complex zeros are immutable and shared; struct/array
     * templates are built once here and every consumer path
     * ({@code AssignNode}, call args, interface boxing) copies value
     * types via {@code copyForAssign}, so sharing is sound — the same
     * discipline phase-1 uses for its own zero templates.
     */
    static Object zeroTemplate(NodeFactory.BuildCtx ctx, int elemId, String path) {
        GoKind kind = kindOf(ctx, elemId, path);
        return zeroTemplateFor(ctx, kind, elemId, path);
    }

    private static Object zeroTemplateFor(NodeFactory.BuildCtx ctx, GoKind kind,
                                          int typeId, String path) {
        switch (kind.slotKind()) {
            case Int -> { return Integer.valueOf(0); }
            case Long -> { return Long.valueOf(0L); }
            case Float -> { return Float.valueOf(0f); }
            case Double -> { return Double.valueOf(0d); }
            case Boolean -> { return Boolean.FALSE; }
            case Object -> {
                if (kind == GoKind.STRING || kind == GoKind.UNTYPED_STRING) {
                    return GoString.EMPTY;
                }
                if (kind == GoKind.COMPLEX64) {
                    return GoComplex32.ZERO;
                }
                if (kind == GoKind.COMPLEX128 || kind == GoKind.UNTYPED_COMPLEX) {
                    return GoComplex64.ZERO;
                }
                if (kind == GoKind.STRUCT) {
                    return structZeroTemplate(ctx, typeId, path);
                }
                if (kind == GoKind.ARRAY) {
                    return arrayZeroTemplate(ctx, typeId, path);
                }
                // Slices, maps, pointers, interfaces, funcs, chans: nil.
                return null;
            }
            default -> throw new FrontendException(
                    path + ": no zero value for kind " + kind + " (WS E)");
        }
    }

    /**
     * Fresh struct zero template with recursively zeroed nested
     * struct/array fields (mirrors
     * {@code NodeFactory.structZeroTemplate}'s recursion; a plain
     * {@code GoStruct.zero} would leave nested struct fields null,
     * diverging from gc's fully-zeroed value).
     */
    private static GoStruct structZeroTemplate(NodeFactory.BuildCtx ctx, int typeId, String path) {
        int sid = structEntryId(ctx, typeId, path);
        GoStructDescriptor desc = structDescriptorFor(ctx, sid, path);
        int[] fieldTypeIds = ctx.structFieldTypeIds.get(sid);
        Object[] objects = new Object[desc.objectsCount()];
        for (int i = 0; i < desc.fields().size(); i++) {
            GoStructDescriptor.Field f = desc.field(i);
            if (!f.inLongs()) {
                objects[f.index()] = switch (f.kind()) {
                    case STRING, UNTYPED_STRING -> GoString.EMPTY;
                    case STRUCT -> structZeroTemplate(ctx, fieldTypeIds[i], path);
                    case ARRAY -> arrayZeroTemplate(ctx, fieldTypeIds[i], path);
                    default -> null;
                };
            }
        }
        return new GoStruct(desc, new long[desc.longsCount()], objects);
    }

    /** Fresh array zero template, recursively initialized (mirrors phase-1). */
    private static Object arrayZeroTemplate(NodeFactory.BuildCtx ctx, int arrayTypeId, String path) {
        int rid = resolveAliasOnly(ctx, arrayTypeId, path);
        Json.Object entry = ctx.types.entry(rid, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": named-type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?)");
            }
            if (!"named".equals(entry.string("kind", path))) {
                break;
            }
            rid = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(rid, path);
        }
        Json.Object detail = entry.object("detail", path);
        int len = detail.intValue("len", path);
        int elemTid = detail.intValue("elem", path);
        GoKind ek = kindOf(ctx, elemTid, path);
        Object arr = newArray(ek, len);
        if (ek == GoKind.ARRAY || ek == GoKind.STRUCT
                || ek == GoKind.STRING || ek == GoKind.UNTYPED_STRING) {
            Object[] objs = (Object[]) arr;
            for (int i = 0; i < len; i++) {
                objs[i] = switch (ek) {
                    case ARRAY -> arrayZeroTemplate(ctx, elemTid, path);
                    case STRUCT -> structZeroTemplate(ctx, elemTid, path).deepCopy();
                    default -> GoString.EMPTY;
                };
            }
        }
        return arr;
    }

    /** Fresh typed Java array for a Go array type (mirrors phase-1's D5 mapping). */
    private static Object newArray(GoKind ek, int len) {
        return switch (ek.slotKind()) {
            case Int -> new int[len];
            case Long -> new long[len];
            case Float -> new float[len];
            case Double -> new double[len];
            case Boolean -> new boolean[len];
            case Object -> new Object[len];
            default -> throw new FrontendException("no array carrier for kind " + ek);
        };
    }

    /**
     * Evaluates an expression node to its canonical boxed value, reading
     * through the typed execute method its {@link GoKind} uses (primitive
     * kinds' {@code executeObject} is not the value carrier — e.g. an int
     * literal's {@code executeObject} is null). Mirrors the
     * {@code AssignNode} dispatch.
     */
    static Object evalBoxed(GoExprNode n, VirtualFrame frame) {
        return switch (n.kind().slotKind()) {
            case Int -> Integer.valueOf(n.executeInt(frame));
            case Long -> Long.valueOf(n.executeLong(frame));
            case Float -> Float.valueOf(n.executeFloat(frame));
            case Double -> Double.valueOf(n.executeDouble(frame));
            case Boolean -> Boolean.valueOf(n.executeBoolean(frame));
            case Object -> n.executeObject(frame);
            default -> throw new FrontendException("cannot read kind " + n.kind() + " (WS E)");
        };
    }

    /**
     * Parks the current goroutine forever, scheduler-aware: the gc-parity
     * behavior of send/recv on a nil channel (gc blocks forever; the
     * scheduler's parked-goroutine accounting is what any future deadlock
     * detector observes — the same park {@code GoChan} itself uses).
     * Never returns normally.
     */
    static void parkForeverNilChannel(ChannelOp op) {
        SchedulerCore sched = SchedulerCore.current();
        // The nil channel has no id; -1 is the established "no single
        // channel" marker (SelectOp uses it for multi-channel select).
        JfrEvents.channelBlock(sched.currentGoroutine(), -1, op);
        sched.parkCurrent(null, ParkReason.CHANNEL);
        throw new IllegalStateException("unreachable: nil-channel " + op + " woke");
    }

    /**
     * Casts a channel value to the {@link ChanOps} handle (fail-fast,
     * never silent). Null is rejected by the callers with gc-parity
     * semantics before this point; a non-channel value here is a broken
     * exporter.
     */
    static ChanOps.Chan asChan(Object ch, String what) {
        if (!(ch instanceof ChanOps.Chan c)) {
            throw new IllegalStateException(what + ": not a channel value ("
                    + (ch == null ? "null" : ch.getClass().getName()) + ")");
        }
        return c;
    }
}
