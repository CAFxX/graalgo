package graalgo.p3math.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code math.Float32frombits(b)} (P3-M7, WS G, ADR-0007 D5): the
 * {@code "mathf32frombits"} wire node.
 *
 * <p>{@link Float#intBitsToFloat} is bit-exact by construction.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class MathF32FromBitsNode extends GoExprNode {

    @Child
    private GoExprNode x;

    private MathF32FromBitsNode(GoExprNode x) {
        super(GoKind.FLOAT32);
        this.x = x;
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return Float.intBitsToFloat(x.executeInt(frame));
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return (double) executeFloat(frame);
    }

    /**
     * Builds the node from the {@code "mathf32frombits"} wire node.
     * Fields: {@code x} (uint32 expression), {@code type} (result
     * type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
                                   NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "mathf32frombits");
        MathWire.requireTypeId(f, "mathf32frombits");
        GoExprNode x = MathWire.operandX(f, "mathf32frombits", factory, ctx);
        return new MathF32FromBitsNode(x);
    }
}
