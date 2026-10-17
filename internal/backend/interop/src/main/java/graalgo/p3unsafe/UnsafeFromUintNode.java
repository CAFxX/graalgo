package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3reflect.ReflectTypes;
import graalgo.values.GoKind;

/**
 * {@code uintptr → unsafe.Pointer} (P3-M8): the {@code "unsafefromuint"}
 * wire node. Traps (D5) unless the operand is a value that already
 * denotes a pointer: a type token (the D5 representation of
 * {@code *abi.Type} — see {@link AbiTypeRef}) round-trips as identity,
 * which is what {@code internal/abi.NoEscape} (the identity function,
 * used by {@code abi.TypeOf}) needs. Anything else traps: a handle can
 * never be materialized from an arbitrary integer. The operand still
 * evaluates (gc evaluates the argument).
 */
public final class UnsafeFromUintNode extends GoExprNode {
    @Child private GoExprNode child;

    private UnsafeFromUintNode(GoExprNode child) {
        super(GoKind.UNSAFE_POINTER);
        this.child = child;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long v = child.executeLong(frame);
        if (v != 0 && isTypeToken(v)) {
            // P3-M10 (eface): a type token already denotes a *abi.Type;
            // NoEscape (semantically identity) round-trips it.
            return v;
        }
        UnsafeOps.fromUintTrap();
        throw new AssertionError("unreachable");
    }

    /** True iff v is a valid reflect type token. */
    private static boolean isTypeToken(long v) {
        try {
            ReflectTypes.table().info(v);
            return true;
        } catch (IllegalArgumentException | IndexOutOfBoundsException e) {
            return false;
        }
    }

    /**
     * Builds the node from the {@code "unsafefromuint"} wire node. Fields:
     * {@code x} (the uintptr operand), {@code type} (result type-table id —
     * the {@code unsafe.Pointer} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafefromuint");
        int typeId = UnsafeWire.requireTypeId(f, "unsafefromuint");
        GoExprNode n = new UnsafeFromUintNode(
                UnsafeWire.child(f, "unsafefromuint", "x", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
