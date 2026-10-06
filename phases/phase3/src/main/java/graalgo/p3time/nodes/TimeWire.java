package graalgo.p3time.nodes;

import graalgo.frontend.ConstNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * Shared wire-format helpers for the P3-M5 time nodes (ADR-0005 D3).
 *
 * <p>Every time node carries {@code type}: the result type-table id (the
 * ADR-0002 {@code syncnew} precedent — never guessed). {@code duration}
 * fields accept either a literal number (nanoseconds) or an expression
 * node evaluating to an int64.
 */
final class TimeWire {
    private TimeWire() {}

    /** The numeric {@code "type"} field (result type-table id). */
    static int requireTypeId(Json.Object f, String kind) {
        String path = kind + ".fields";
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": " + kind + " needs a numeric \"type\" id");
        }
        return (int) num.asLong(path + ".type");
    }

    /** The {@code "duration"} field as an int64-valued expression. */
    static GoExprNode durationNode(Json.Object f, String kind,
                                   NodeFactory factory, NodeFactory.BuildCtx ctx) {
        String path = kind + ".fields";
        Json.Value v = f.opt("duration");
        if (v == null) {
            throw new FrontendException(path + ": " + kind + " needs \"duration\"");
        }
        if (v instanceof Json.Num num) {
            return ConstNode.ofInt(GoKind.INT64, num.asLong(path + ".duration"));
        }
        if (v instanceof Json.Object o) {
            return factory.buildExpr(o, ctx);
        }
        throw new FrontendException(path + ".duration: must be a number or a node object");
    }

    /** Validates that a type-table entry has the expected composite kind. */
    static void expectKind(NodeFactory.BuildCtx ctx, int typeId, String kind,
                           String expected, String what) {
        String path = kind + ".fields";
        Json.Object entry = ctx.types.entry(typeId, path);
        String k = entry.string("kind", path);
        if (!expected.equals(k)) {
            throw new FrontendException(
                    path + ": " + kind + " " + what + " must be " + expected + ", got " + k);
        }
    }
}
