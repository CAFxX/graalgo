package graalgo.p3math.nodes;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;

/**
 * Shared wire-format helpers for the P3-M7 math nodes (ADR-0007 D5).
 *
 * <p>Every math node carries {@code type}: the result type-table id (the
 * ADR-0002 {@code syncnew} precedent — never guessed). Unary nodes take
 * {@code x}; the FMA node takes {@code x}, {@code y}, {@code z}.
 */
final class MathWire {
    private MathWire() {}

    /** The numeric {@code "type"} field (result type-table id). */
    static int requireTypeId(Json.Object f, String kind) {
        String path = kind + ".fields";
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": " + kind + " needs a numeric \"type\" id");
        }
        return (int) num.asLong(path + ".type");
    }

    /** The {@code "x"} operand as a {@link GoExprNode}. */
    static GoExprNode operandX(Json.Object f, String kind,
                               NodeFactory factory, NodeFactory.BuildCtx ctx) {
        return operand(f, kind, "x", factory, ctx);
    }

    /** A named operand ({@code x}, {@code y}, {@code z}) as a {@link GoExprNode}. */
    static GoExprNode operand(Json.Object f, String kind, String name,
                              NodeFactory factory, NodeFactory.BuildCtx ctx) {
        String path = kind + ".fields";
        Json.Value v = f.opt(name);
        if (v == null) {
            throw new FrontendException(path + ": " + kind + " needs \"" + name + "\"");
        }
        if (v instanceof Json.Object o) {
            return factory.buildExpr(o, ctx);
        }
        throw new FrontendException(path + "." + name + ": must be a node object");
    }
}
