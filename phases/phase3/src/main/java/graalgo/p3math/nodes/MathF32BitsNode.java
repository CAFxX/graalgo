package graalgo.p3math.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code math.Float32bits(f)} (P3-M7, WS G, ADR-0007 D5): the
 * {@code "mathf32bits"} wire node.
 *
 * <p>{@link Float#floatToRawIntBits} is bit-exact by construction.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class MathF32BitsNode extends GoExprNode {

    @Child
    private GoExprNode x;

    private MathF32BitsNode(GoExprNode x) {
        super(GoKind.UINT32);
        this.x = x;
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return Float.floatToRawIntBits(x.executeFloat(frame));
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return Integer.toUnsignedLong(executeInt(frame));
    }

    /**
     * Builds the node from the {@code "mathf32bits"} wire node. Fields:
     * {@code x} (float32 expression), {@code type} (result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
                                   NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "mathf32bits");
        MathWire.requireTypeId(f, "mathf32bits");
        GoExprNode x = MathWire.operandX(f, "mathf32bits", factory, ctx);
        return new MathF32BitsNode(x);
    }
}
