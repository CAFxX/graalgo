package graalgo.iface;

import java.util.ArrayList;
import java.util.List;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.InterfaceBoxNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * Builds {@link IfaceDispatchNode} from the {@code "ifacemethod"} wire node
 * (P3-M3, WS D, ADR-0003).
 *
 * <p>Wire fields: {@code recv} (evaluates to a {@code GoInterface}),
 * {@code method} (name), {@code iface} (interface type-table id — the
 * method list source), {@code args} (call args, receiver NOT included),
 * {@code sig} (method signature type-table id, <em>no receiver</em>, for
 * call-site arg adaptation), {@code type} (result type id, null when no
 * results).
 *
 * <p>Call-site arg adaptation replicates the factory's private
 * {@code adaptArgsForInterfaceParams} / {@code boxInterface} (read as the
 * reference implementation; not modified — frozen): when a callee
 * parameter has interface type and the argument is a concrete value, the
 * argument is boxed with its exact static-type descriptor via the public
 * {@link InterfaceBoxNode} constructor. The descriptor comes from the
 * public {@code ctx.typeDescs} cache when interned, from the public
 * {@link NodeFactory#basicDescriptorFor} for basic/untyped kinds
 * (canonical), and otherwise from the public
 * {@link NodeFactory#canonicalDescriptorFor} accessor (R1 P0-2) — the
 * miss path is routine, not exceptional: structural descriptors (notably
 * every {@code &T{...}} pointer argument) live only in the factory's
 * process-wide structural cache, never in the per-function
 * {@code typeDescs} map. The miss-path descriptor is object-identical to
 * the factory's canonical instance, so type switches/asserts and
 * interface {@code ==} inside the callee behave exactly as on
 * factory-boxed values. Dispatch keys on the descriptor's qualified
 * method-table key ({@code GoTypeDescriptor.methodTableKey()}), never
 * the display name (N-P1-2).
 *
 * <p>Registered via the public {@code NodeFactory.registerExprKind}
 * hook — no Phase-1 change (beyond the ADR-0003-documented
 * {@code canonicalDescriptorFor} accessor).
 *
 * <p>verifies: GG-0017 (iface slice)
 */
public final class IfaceDispatchBuilder implements NodeFactory.ExprBuilder {
    private final NodeFactory factory;

    public IfaceDispatchBuilder(NodeFactory factory) {
        this.factory = factory;
    }

    @Override
    public GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "ifacemethod");
        String path = "ifacemethod.fields";
        GoExprNode recv = factory.buildExpr(f.object("recv", path), ctx);
        String method = f.string("method", path);
        int ifaceId = f.intValue("iface", path);
        if (recv.kind() != GoKind.INTERFACE) {
            // P3-M9 (WS H + WS D): type-parameter receivers from
            // exportIfaceFallbackCall build to the concrete kind (e.g.
            // POINTER for *nistec.P256Point), not INTERFACE. Box the
            // concrete value as the constraint interface for itab dispatch.
            recv = boxArg(recv, ifaceId, ctx, path);
        }
        Json.Array arr = f.array("args", path);
        GoExprNode[] args = new GoExprNode[arr.size()];
        for (int i = 0; i < args.length; i++) {
            Json.Value v = arr.get(i);
            if (!(v instanceof Json.Object o)) {
                throw new FrontendException(path + ".args[" + i + "]: must be a node object");
            }
            args[i] = factory.buildExpr(o, ctx);
        }
        List<String> ifaceMethodNames = ifaceMethodNames(ctx, ifaceId, path);
        args = adaptArgsForInterfaceParams(args, signatureParamTypeIds(ctx, f, path), ctx, path);
        return new IfaceDispatchNode(recv, args, method, ifaceMethodNames, ifaceId,
                resultKind(ctx, f, path), ctx.funcObjId);
    }

    /**
     * Interface method names in declaration order, from the interface
     * type-table entry's {@code detail.methods} (the exporter flattens
     * embedded interfaces via go/types' method set).
     */
    private static List<String> ifaceMethodNames(NodeFactory.BuildCtx ctx, int ifaceId, String path) {
        Json.Object entry = ctx.types.entry(ifaceId, path);
        if (!"interface".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": ifacemethod \"iface\" id " + ifaceId
                    + " is not an interface type");
        }
        Json.Array methods = entry.object("detail", path).array("methods", path);
        List<String> names = new ArrayList<>(methods.size());
        for (int i = 0; i < methods.size(); i++) {
            Json.Value mv = methods.get(i);
            if (!(mv instanceof Json.Object mo)) {
                throw new FrontendException(path + ": interface method entry " + i + " not an object");
            }
            names.add(mo.string("name", path));
        }
        return names;
    }

    /**
     * Result kind from the wire {@code "type"} id (null → void: dummy
     * kind, mirroring the factory's {@code callResultKind}).
     */
    private static GoKind resultKind(NodeFactory.BuildCtx ctx, Json.Object f, String path) {
        Json.Value t = f.opt("type");
        if (t == null || t instanceof Json.Null) {
            return GoKind.INTERFACE;
        }
        if (!(t instanceof Json.Num n)) {
            throw new FrontendException(path + ": ifacemethod \"type\" must be a type id or null");
        }
        return DescriptorNames.kindOf(ctx.types, (int) n.asLong(path), path);
    }

    /**
     * Replica of the factory's private {@code signatureParamTypeIds}: the
     * callee's parameter type ids, or null when the signature is absent
     * or unreadable. Variadic signatures adapt too (N-P1-1, mirroring
     * the factory's fix-redmaster-r40 fix): the exporter always emits a
     * variadic call in packed or spread form, so the call's arguments
     * align 1:1 with the signature's params and the last param — the
     * variadic slice type itself — is never an interface, hence never
     * boxed. The previous variadic bail-out skipped adaptation for the
     * FIXED params too, passing raw concrete values into interface
     * slots on the iface path (the same failure mode r40 fixed on the
     * factory path).
     */
    private static List<Integer> signatureParamTypeIds(NodeFactory.BuildCtx ctx,
            Json.Object f, String path) {
        Json.Value sig = f.opt("sig");
        if (sig == null || sig instanceof Json.Null) {
            return null;
        }
        if (!(sig instanceof Json.Num n)) {
            throw new FrontendException(path + ": ifacemethod \"sig\" must be a type id");
        }
        int sigId = (int) n.asLong(path);
        Json.Object entry = ctx.types.entry(sigId, path);
        if (!"signature".equals(entry.string("kind", path))) {
            return null;
        }
        Json.Object detail = entry.object("detail", path);
        Json.Array params = detail.array("params", path);
        List<Integer> out = new ArrayList<>(params.size());
        for (int i = 0; i < params.size(); i++) {
            Json.Value pv = params.get(i);
            if (!(pv instanceof Json.Object po)) {
                return null;
            }
            out.add(po.intValue("type", path));
        }
        return out;
    }

    /**
     * Replica of the factory's private {@code adaptArgsForInterfaceParams}:
     * box concrete args passed to interface-typed params. The wire
     * {@code sig} carries no receiver (unlike methodref), so args align
     * 1:1 with params; the receiver is already a {@code GoInterface}.
     */
    private static GoExprNode[] adaptArgsForInterfaceParams(GoExprNode[] args,
            List<Integer> paramTypeIds, NodeFactory.BuildCtx ctx, String path) {
        if (paramTypeIds == null) {
            return args;
        }
        for (int i = 0; i < args.length && i < paramTypeIds.size(); i++) {
            if (args[i].kind() == GoKind.INTERFACE) {
                continue; // already an interface value
            }
            // Named interface types (e.g. main.Writer) box too — the
            // factory's adaptArgsForInterfaceParams had the same gap
            // (fixed P3-M3 in NodeFactory.isInterfaceType).
            if (!isInterfaceType(ctx, paramTypeIds.get(i), path)) {
                continue;
            }
            args[i] = boxArg(args[i], paramTypeIds.get(i), ctx, path);
        }
        return args;
    }

    /**
     * P3-M3: true when the type-table id denotes an interface type,
     * following {@code named}→{@code underlying} (mirrors the factory's
     * {@code NodeFactory.isInterfaceType}; kept local — the factory's is
     * package-private).
     */
    private static boolean isInterfaceType(NodeFactory.BuildCtx ctx, int typeId, String path) {
        typeId = DescriptorNames.resolveAlias(ctx.types, typeId, path);
        for (int steps = 0; steps < 10_000; steps++) {
            Json.Object entry = ctx.types.entry(typeId, path);
            String k = entry.string("kind", path);
            if ("interface".equals(k)) {
                return true;
            }
            if (!"named".equals(k)) {
                return false;
            }
            typeId = entry.object("detail", path).intValue("underlying", path);
        }
        throw new FrontendException(path + ": type chain exceeds bound following named→underlying");
    }

    /**
     * Replica of the factory's private {@code boxInterface}: box a
     * concrete value into an interface with its exact static-type
     * descriptor. {@code toTypeId} is the interface parameter's type id,
     * for the R1 P1-2 generic-instantiation scope check.
     */
    private static GoExprNode boxArg(GoExprNode child, int toTypeId, NodeFactory.BuildCtx ctx,
            String path) {
        GoKind from = child.kind();
        GoKind dataKind;
        GoTypeDescriptor descriptor;
        if (from.isUntyped()) {
            // P3-M7R2: delegate both the kind and the descriptor to the
            // factory — single source of truth.
            dataKind = NodeFactory.defaultKind(from);
            // P3-M7R2: delegate to the factory's default-type descriptor
            // ("int", not "int64") so this replica mints the identical
            // instance the factory's boxInterface mints.
            descriptor = NodeFactory.untypedDefaultDescriptor(ctx, child, dataKind, path);
        } else {
            dataKind = from;
            int fromId = child.typeId();
            if (fromId < 0) {
                throw new FrontendException(path
                        + ": cannot box a node without a type-table id (WS E)");
            }
            // P3-M3 remediation (R1 P1-2): a generic instantiation boxed
            // into a non-empty interface fails fast with the named P3-M3
            // scope error (its wire method table is empty by design).
            NodeFactory.checkInstantiationBoxedAsInterface(ctx, fromId, toTypeId, path);
            descriptor = ctx.typeDescs.get(fromId);
            if (descriptor == null) {
                descriptor = replicaDescriptor(ctx, fromId, path);
            }
        }
        InterfaceBoxNode n = new InterfaceBoxNode(child, dataKind, descriptor);
        n.setSourceSection(child.getSourceSection());
        n.setTypeId(child.typeId());
        return n;
    }

    /**
     * The factory's canonical descriptor for the {@code ctx.typeDescs}
     * miss path (R1 P0-2): the miss is normal, not exceptional — pointer
     * (and other structural) descriptors live only in the factory's
     * process-wide structural cache, never in the per-function
     * {@code typeDescs} map, so boxing a {@code &T{...}} argument misses
     * routinely. The returned instance is object-identical to the one the
     * factory itself produces for the same type id, so type
     * assertions/switches/interface {@code ==} on the boxed value behave
     * exactly as on factory-boxed values.
     */
    private static GoTypeDescriptor replicaDescriptor(NodeFactory.BuildCtx ctx, int fromId,
            String path) {
        return NodeFactory.canonicalDescriptorFor(ctx, fromId, path);
    }
}
