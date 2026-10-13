package graalgo.p3math.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code math.FMA(x, y, z)} (P3-M7, WS G, ADR-0007 D5): the
 * {@code "mathfma"} wire node.
 *
 * <p>{@link Math#fma} is specified correctly-rounded (single rounding,
 * as if infinite precision) — matching gc's software FMA bit-exactly
 * (differentially verified: 0 mismatches / 20000 adversarial+random
 * inputs, hidden_files/m7-math). There is no {@code StrictMath.fma};
 * {@code Math.fma}'s spec is the guarantee.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class MathFmaNode extends GoExprNode {

    @Child
    private GoExprNode x;
    @Child
    private GoExprNode y;
    @Child
    private GoExprNode z;

    private MathFmaNode(GoExprNode x, GoExprNode y, GoExprNode z) {
        super(GoKind.FLOAT64);
        this.x = x;
        this.y = y;
        this.z = z;
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return Math.fma(x.executeDouble(frame), y.executeDouble(frame),
                        z.executeDouble(frame));
    }

    /**
     * Builds the node from the {@code "mathfma"} wire node. Fields:
     * {@code x}, {@code y}, {@code z} (float64 expressions), {@code type}
     * (result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
                                   NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "mathfma");
        MathWire.requireTypeId(f, "mathfma");
        GoExprNode x = MathWire.operandX(f, "mathfma", factory, ctx);
        GoExprNode y = MathWire.operand(f, "mathfma", "y", factory, ctx);
        GoExprNode z = MathWire.operand(f, "mathfma", "z", factory, ctx);
        return new MathFmaNode(x, y, z);
    }
}
