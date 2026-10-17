package graalgo.p3builtin.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.Boxing;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoStructDescriptor;

/**
 * The {@code clear} builtin (Go 1.21+): the {@code "clear"} wire node.
 *
 * <p>The frozen v1 builtin surface rejects {@code clear} (WS G), but real
 * modules use it, so the frontend lowers it to this additive wire node —
 * the same pattern as {@code "minmax"}. No Phase-1 {@code NodeFactory}
 * change.
 *
 * <p>Semantics follow gc exactly:
 * <ul>
 *   <li>{@code clear(m)} on a map deletes all entries; a nil map is a
 *       no-op;</li>
 *   <li>{@code clear(s)} on a slice zeroes the {@code len(s)} elements
 *       (the backing array beyond the length is untouched); a nil slice
 *       is a no-op.</li>
 * </ul>
 *
 * <p>The operand kind (map vs slice) is resolved from the runtime value,
 * never from the type table. {@code clear} returns no value; the node
 * evaluates to {@code null} and is used in statement position.
 *
 * <p>Slice zeroing goes through {@link Boxing#clearSlice}, the single
 * canonical implementation covering every D5 backing carrier; the
 * element kind (and struct descriptor) are resolved from the type table
 * at build time so {@code Object[]} slots get the Go zero value, not
 * Java {@code null} (I-8).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class ClearNode extends GoExprNode {

    @Child private GoExprNode operand;
    /** Slice element kind (for the {@code Object[]} zero value). */
    private final GoKind elemKind;
    /** Element struct descriptor for {@code STRUCT} kinds; null otherwise. */
    private final GoStructDescriptor elemDesc;

    ClearNode(GoExprNode operand, GoKind elemKind, GoStructDescriptor elemDesc) {
        // clear returns no Go value; the kind is a placeholder (the
        // DeleteNode precedent uses INT64 for the same reason). The node
        // is only ever used in statement position.
        super(graalgo.values.GoKind.INT64);
        this.operand = operand;
        this.elemKind = elemKind;
        this.elemDesc = elemDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        doClear(frame);
        return null;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // clear is statement-only, but ExprStmtNode dispatches on
        // kind().slotKind(): INT64 -> executeLong. The truffle-frontend
        // ClearNode implements executeLong for the same reason.
        doClear(frame);
        return 0L;
    }

    /** Performs the clear; shared by executeObject/executeLong. */
    private void doClear(VirtualFrame frame) {
        Object v = operand.executeObject(frame);
        if (v == null) {
            return; // clear(nil) is a no-op.
        }
        if (v instanceof GoMap m) {
            m.clear();
            return;
        }
        if (v instanceof GoSlice s) {
            Boxing.clearSlice(s, elemKind, elemDesc);
            return;
        }
        throw new IllegalStateException(
                "clear of non-map, non-slice value: " + v.getClass());
    }

    /**
     * Builds the node from the {@code "clear"} wire node. Fields:
     * {@code x} (the operand expression).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "clear");
        String path = "clear.fields";
        Json.Value xv = f.opt("x");
        if (!(xv instanceof Json.Object o)) {
            throw new FrontendException(path + ": clear needs an \"x\" node object");
        }
        GoExprNode operand = factory.buildExpr(o, ctx);
        // Resolve the slice's element kind/descriptor for the Object[]
        // zero value. Unresolvable (missing type id) degrades to
        // INTERFACE — nulling, the pre-fix behavior.
        GoKind elemKind = GoKind.INTERFACE;
        GoStructDescriptor elemDesc = null;
        int sliceTid = operand.typeId();
        if (sliceTid >= 0 && operand.kind() == GoKind.SLICE) {
            int elemTid = NodeFactory.sliceElemTypeId(ctx, sliceTid, path, "clear");
            elemKind = NodeFactory.kindOf(ctx, elemTid, path);
            if (elemKind == GoKind.STRUCT) {
                elemDesc = NodeFactory.structDescriptorFor(ctx, elemTid, path);
            }
        }
        return new ClearNode(operand, elemKind, elemDesc);
    }
}
