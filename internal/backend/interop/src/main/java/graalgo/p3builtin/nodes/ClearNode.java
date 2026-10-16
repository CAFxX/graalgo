package graalgo.p3builtin.nodes;

import java.lang.reflect.Array;
import java.util.Arrays;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

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
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class ClearNode extends GoExprNode {

    @Child private GoExprNode operand;

    private ClearNode(GoExprNode operand) {
        // clear returns no Go value; the kind is a placeholder (the
        // DeleteNode precedent uses INT64 for the same reason). The node
        // is only ever used in statement position.
        super(graalgo.values.GoKind.INT64);
        this.operand = operand;
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
            clearSlice(s);
            return;
        }
        throw new IllegalStateException(
                "clear of non-map, non-slice value: " + v.getClass());
    }

    /** Zeroes the slice's len elements (backing array shared — no copy). */
    private static void clearSlice(GoSlice s) {
        Object array = s.array();
        int from = s.offset();
        int len = s.len();
        if (len == 0) {
            return;
        }
        Class<?> ct = array.getClass().getComponentType();
        if (!ct.isPrimitive()) {
            Arrays.fill((Object[]) array, from, from + len, null);
            return;
        }
        Object zero = primitiveZero(ct);
        for (int i = 0; i < len; i++) {
            Array.set(array, from + i, zero);
        }
    }

    /** The boxed zero for a primitive component type. */
    private static Object primitiveZero(Class<?> ct) {
        if (ct == boolean.class) {
            return Boolean.FALSE;
        }
        if (ct == byte.class) {
            return (byte) 0;
        }
        if (ct == short.class) {
            return (short) 0;
        }
        if (ct == int.class) {
            return 0;
        }
        if (ct == long.class) {
            return 0L;
        }
        if (ct == float.class) {
            return 0.0f;
        }
        if (ct == double.class) {
            return 0.0;
        }
        if (ct == char.class) {
            return '\0';
        }
        throw new IllegalStateException("unsupported primitive component type: " + ct);
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
        return new ClearNode(factory.buildExpr(o, ctx));
    }
}
