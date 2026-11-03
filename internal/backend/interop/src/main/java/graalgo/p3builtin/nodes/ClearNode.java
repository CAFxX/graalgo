package graalgo.p3builtin.nodes;

import java.lang.reflect.Array;
import java.util.Arrays;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;

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
 * <p>REV-2026-11-03-15 P1-2: slice elements are zeroed with the Go zero
 * value, not Java null. The wire node carries the slice element type id
 * in {@code "elem"} (frontend, export.go clear case); the build resolves
 * it to an element kind and, for struct elements, to a zero template
 * (the same {@code GoStruct.zero} state {@code make} produces for a
 * struct slice). String elements fill with {@code GoString.EMPTY},
 * complex elements with the (0+0i) constants, and pointer/interface/
 * map/func/chan/slice elements — whose Go zero genuinely is nil —
 * with null. (Array elements keep the null slots {@code make} gives
 * them today; see {@code clearObjectSlice}.) A slice operand without
 * a resolvable element type fails
 * loudly (build time when the operand is statically known to be a
 * slice, first execution otherwise) instead of silently null-filling:
 * a null string/struct/complex slot would surface in guest code as a
 * host NullPointerException or a nil where gc yields a zero value.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class ClearNode extends GoExprNode {

    @Child private GoExprNode operand;

    /** Slice element kind from the wire {@code "elem"} id; null when absent. */
    private final GoKind elemKind;

    /**
     * Zero template for STRUCT elements (null otherwise): each cleared
     * slot gets {@link GoStruct#deepCopy()} of this template, never the
     * template itself — sharing one instance would alias mutations
     * across slots (fix-redmaster-r42 precedent).
     */
    private final GoStruct elemStructZeroTemplate;

    private ClearNode(GoExprNode operand, GoKind elemKind, GoStruct elemStructZeroTemplate) {
        // clear returns no Go value; the kind is a placeholder (the
        // DeleteNode precedent uses INT64 for the same reason). The node
        // is only ever used in statement position.
        super(graalgo.values.GoKind.INT64);
        this.operand = operand;
        this.elemKind = elemKind;
        this.elemStructZeroTemplate = elemStructZeroTemplate;
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
    private void clearSlice(GoSlice s) {
        Object array = s.array();
        int from = s.offset();
        int len = s.len();
        if (len == 0) {
            return;
        }
        Class<?> ct = array.getClass().getComponentType();
        if (!ct.isPrimitive()) {
            clearObjectSlice((Object[]) array, from, len);
            return;
        }
        Object zero = primitiveZero(ct);
        for (int i = 0; i < len; i++) {
            Array.set(array, from + i, zero);
        }
    }

    /**
     * Zeroes {@code len} Object[] slots starting at {@code from} with the
     * Go zero value for the element kind (see class doc). Pointer,
     * interface, map, func, chan, and slice elements genuinely zero to
     * nil, matching the state {@code make} produces; array elements keep
     * the make-time null slots (their allocation gap, not this node's,
     * to fix).
     */
    private void clearObjectSlice(Object[] array, int from, int len) {
        if (elemKind == null) {
            // The wire node carried no element type and the frontend
            // could not prove the operand is a map. Never guess: a
            // silent null-fill would corrupt string/struct/complex
            // slices. This is unreachable for a typechecked program
            // (the frontend attaches "elem" for every slice argument
            // whose type it knows, and build() rejects a statically
            // known slice without one).
            throw new IllegalStateException(
                    "clear of slice without element type (frontend omitted \"elem\")");
        }
        switch (elemKind) {
            case STRING, UNTYPED_STRING ->
                    Arrays.fill(array, from, from + len, GoString.EMPTY);
            case COMPLEX64 ->
                    Arrays.fill(array, from, from + len, GoComplex32.ZERO);
            case COMPLEX128, UNTYPED_COMPLEX ->
                    Arrays.fill(array, from, from + len, GoComplex64.ZERO);
            case STRUCT -> {
                if (elemStructZeroTemplate == null) {
                    throw new IllegalStateException(
                            "clear of struct slice without a zero template");
                }
                for (int i = 0; i < len; i++) {
                    array[from + i] = elemStructZeroTemplate.deepCopy();
                }
            }
            default -> Arrays.fill(array, from, from + len, null);
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
     * {@code x} (the operand expression) and, for slice operands,
     * {@code elem} (the slice element type-table id). Map operands carry
     * no {@code elem}; a statically-known slice operand without one is
     * a frontend bug and fails here, at build time, rather than
     * null-filling at execution time. An unresolvable element kind or
     * struct descriptor likewise fails here via
     * {@code NodeFactory.kindOf} / {@code structDescriptorFor}.
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
        Json.Value ev = f.opt("elem");
        if (ev == null) {
            if (operand.kind() == GoKind.SLICE) {
                throw new FrontendException(
                        path + ": clear of a slice needs an \"elem\" type id");
            }
            // Map operand (or an operand kind the build cannot prove):
            // no element type needed. A slice that reaches execution
            // without one throws loudly in clearObjectSlice instead of
            // null-filling.
            return new ClearNode(operand, null, null);
        }
        if (!(ev instanceof Json.Num num)) {
            throw new FrontendException(path + ": clear \"elem\" must be a type id");
        }
        int elemTid = (int) num.asLong(path + ".elem");
        GoKind elemKind = NodeFactory.kindOf(ctx, elemTid, path);
        GoStruct structTemplate = null;
        if (elemKind == GoKind.STRUCT) {
            // Same zero state make() produces for this struct type
            // (GoStruct.zero, recursively zeroed); per-slot deep copies
            // are made at execution time.
            structTemplate = GoStruct.zero(NodeFactory.structDescriptorFor(ctx, elemTid, path));
        }
        return new ClearNode(operand, elemKind, structTemplate);
    }
}
