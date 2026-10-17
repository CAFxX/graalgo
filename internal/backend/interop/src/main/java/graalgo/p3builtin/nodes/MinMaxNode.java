package graalgo.p3builtin.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * The {@code min}/{@code max} builtins (Go 1.21+): the {@code "minmax"}
 * wire node.
 *
 * <p>The frozen v1 builtin surface rejects {@code min}/{@code max} (WS G),
 * but real modules use them, so the frontend lowers them to this additive
 * wire node — the same pattern as {@code "syncnew"}/{@code "makechan"}.
 * No Phase-1 {@code NodeFactory} change.
 *
 * <p>Semantics follow gc exactly:
 * <ul>
 *   <li>one argument: returns it;</li>
 *   <li>integers (signed and unsigned): numeric fold;</li>
 *   <li>floats: NaN-propagating — any NaN argument yields NaN;</li>
 *   <li>floats: signed-zero tie rule — {@code min} prefers {@code -0.0},
 *       {@code max} prefers {@code +0.0} (IEEE total order on the non-NaN
 *       range; observable via {@code math.Signbit}/{@code math.Float64bits});</li>
 *   <li>strings: byte-wise lexicographic (via {@link GoString#compareTo}).</li>
 * </ul>
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class MinMaxNode extends GoExprNode {

    @Children private final GoExprNode[] args;
    private final boolean isMin;

    private MinMaxNode(GoKind kind, boolean isMin, GoExprNode[] args) {
        super(kind);
        this.isMin = isMin;
        this.args = args;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        if (!kind().isIntKind() || kind().isFloatKind()) {
            throw new IllegalStateException("minmax executeLong on non-int kind " + kind());
        }
        boolean unsigned = kind().isUintKind();
        long best = args[0].executeLong(frame);
        for (int i = 1; i < args.length; i++) {
            long v = args[i].executeLong(frame);
            boolean take;
            if (unsigned) {
                take = isMin
                        ? Long.compareUnsigned(v, best) < 0
                        : Long.compareUnsigned(v, best) > 0;
            } else {
                take = isMin ? v < best : v > best;
            }
            if (take) {
                best = v;
            }
        }
        return best;
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        if (kind() != GoKind.FLOAT32) {
            throw new IllegalStateException("minmax executeFloat on non-float32 kind " + kind());
        }
        float best = args[0].executeFloat(frame);
        for (int i = 1; i < args.length; i++) {
            float v = args[i].executeFloat(frame);
            if (Float.isNaN(v) || Float.isNaN(best)) {
                return Float.NaN;
            }
            // RV-P0-08: Float.compare distinguishes -0.0f from +0.0f (IEEE
            // total order on the non-NaN range), so min prefers -0.0f and
            // max prefers +0.0f — gc parity. Plain < / > treats -0.0f == +0.0f
            // and keeps the wrong zero.
            if (isMin ? Float.compare(v, best) < 0 : Float.compare(v, best) > 0) {
                best = v;
            }
        }
        return best;
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        if (kind() != GoKind.FLOAT64 && kind() != GoKind.UNTYPED_FLOAT) {
            throw new IllegalStateException("minmax executeDouble on non-float64 kind " + kind());
        }
        double best = args[0].executeDouble(frame);
        for (int i = 1; i < args.length; i++) {
            double v = args[i].executeDouble(frame);
            if (Double.isNaN(v) || Double.isNaN(best)) {
                return Double.NaN;
            }
            // RV-P0-08: Double.compare distinguishes -0.0 from +0.0 (IEEE
            // total order on the non-NaN range), so min prefers -0.0 and
            // max prefers +0.0 — gc parity. Plain < / > treats -0.0 == +0.0
            // and keeps the wrong zero.
            if (isMin ? Double.compare(v, best) < 0 : Double.compare(v, best) > 0) {
                best = v;
            }
        }
        return best;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (kind() != GoKind.STRING && kind() != GoKind.UNTYPED_STRING) {
            throw new IllegalStateException("minmax executeObject on non-string kind " + kind());
        }
        GoString best = (GoString) args[0].executeObject(frame);
        for (int i = 1; i < args.length; i++) {
            GoString v = (GoString) args[i].executeObject(frame);
            int cmp = v.compareTo(best);
            if (isMin ? cmp < 0 : cmp > 0) {
                best = v;
            }
        }
        return best;
    }

    /**
     * Builds the node from the {@code "minmax"} wire node. Fields:
     * {@code op} ("min"/"max"), {@code args} (array of expression nodes),
     * {@code type} (result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "minmax");
        String path = "minmax.fields";
        String op = f.string("op", path);
        final boolean isMin;
        switch (op) {
            case "min" -> isMin = true;
            case "max" -> isMin = false;
            default -> throw new FrontendException(path + ": unknown minmax op " + op);
        }
        Json.Array arr = f.array("args", path);
        if (arr.size() < 1) {
            throw new FrontendException(path + ": minmax needs at least one argument");
        }
        GoExprNode[] argNodes = new GoExprNode[arr.size()];
        for (int i = 0; i < arr.size(); i++) {
            Json.Value v = arr.get(i);
            if (!(v instanceof Json.Object o)) {
                throw new FrontendException(path + ".args[" + i + "]: must be a node object");
            }
            argNodes[i] = factory.buildExpr(o, ctx);
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": minmax needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoKind kind = NodeFactory.kindOf(ctx, typeId, path);
        if (!kind.isIntKind() && !kind.isFloatKind()
                && kind != GoKind.STRING && kind != GoKind.UNTYPED_STRING) {
            throw new FrontendException(
                    path + ": minmax of unsupported kind " + kind + " (want ordered)");
        }
        return new MinMaxNode(kind, isMin, argNodes);
    }
}
