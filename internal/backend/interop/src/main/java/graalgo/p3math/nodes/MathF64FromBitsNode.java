package graalgo.p3math.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code math.Float64frombits(b)} (P3-M7, WS G, ADR-0007 D5): the
 * {@code "mathf64frombits"} wire node.
 *
 * <p>{@link Double#longBitsToDouble} is bit-exact by construction.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class MathF64FromBitsNode extends GoExprNode {

    @Child
    private GoExprNode x;

    private MathF64FromBitsNode(GoExprNode x) {
        super(GoKind.FLOAT64);
        this.x = x;
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return Double.longBitsToDouble(x.executeLong(frame));
    }

    /**
     * Builds the node from the {@code "mathf64frombits"} wire node.
     * Fields: {@code x} (uint64 expression), {@code type} (result
     * type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
                                   NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "mathf64frombits");
        MathWire.requireTypeId(f, "mathf64frombits");
        GoExprNode x = MathWire.operandX(f, "mathf64frombits", factory, ctx);
        return new MathF64FromBitsNode(x);
    }
}
