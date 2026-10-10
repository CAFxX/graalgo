package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoString;

// verifies: GG-0022 (WS G builtin)
// P3-M9: min/max builtins (Go 1.21+).
/**
 * The {@code min} and {@code max} builtins (WS G; verifies GG-0022).
 *
 * <p>{@code min(x, y, ...)} returns the smallest argument, {@code max} the
 * largest. All arguments have the same ordered type (gc type-checks this;
 * at least one argument is required). Each argument is evaluated exactly
 * once, in order.
 *
 * <p>Float semantics match gc: if any argument is NaN, the result is NaN
 * (the first NaN argument's bits are returned). For -0.0/+0.0 ties,
 * {@code min} returns -0.0 and {@code max} returns +0.0.
 */
public final class MinMaxNode extends GoExprNode {
    private final boolean isMin;
    @Children private final GoExprNode[] args;

    public MinMaxNode(GoKind kind, boolean isMin, GoExprNode[] args) {
        super(kind);
        this.isMin = isMin;
        this.args = args;
    }

    /**
     * Evaluates all args once and returns the winning value, canonically
     * boxed (sub-64-bit ints as Integer, 64-bit as Long, float32 as Float,
     * float64 as Double, strings as GoString).
     */
    private Object computeBest(VirtualFrame frame) {
        GoKind k = kind();
        if (k == GoKind.STRING) {
            GoString best = (GoString) args[0].executeObject(frame);
            for (int i = 1; i < args.length; i++) {
                GoString cur = (GoString) args[i].executeObject(frame);
                int cmp = cur.compareTo(best);
                if ((isMin && cmp < 0) || (!isMin && cmp > 0)) {
                    best = cur;
                }
            }
            return best;
        }
        if (k.isFloatKind()) {
            double[] vals = new double[args.length];
            for (int i = 0; i < args.length; i++) {
                vals[i] = args[i].executeDouble(frame);
            }
            // NaN propagates (gc parity): return the first NaN.
            for (double v : vals) {
                if (Double.isNaN(v)) {
                    return k == GoKind.FLOAT32 ? Float.valueOf((float) v) : Double.valueOf(v);
                }
            }
            // Double.compare distinguishes -0.0 from +0.0, giving gc's
            // min(-0,+0) = -0 and max(-0,+0) = +0.
            int best = 0;
            for (int i = 1; i < vals.length; i++) {
                int cmp = Double.compare(vals[i], vals[best]);
                if ((isMin && cmp < 0) || (!isMin && cmp > 0)) {
                    best = i;
                }
            }
            double bv = vals[best];
            return k == GoKind.FLOAT32 ? Float.valueOf((float) bv) : Double.valueOf(bv);
        }
        // Integer kinds (signed and unsigned). executeLong handles
        // sign/zero extension per kind (D5).
        boolean unsigned = k.isUintKind();
        long[] vals = new long[args.length];
        for (int i = 0; i < args.length; i++) {
            vals[i] = args[i].executeLong(frame);
        }
        int best = 0;
        for (int i = 1; i < vals.length; i++) {
            int cmp = unsigned ? Long.compareUnsigned(vals[i], vals[best])
                               : Long.compare(vals[i], vals[best]);
            if ((isMin && cmp < 0) || (!isMin && cmp > 0)) {
                best = i;
            }
        }
        long bv = vals[best];
        return switch (k) {
            case INT64, UINT64, UNTYPED_INT -> Long.valueOf(bv);
            default -> Integer.valueOf((int) bv);
        };
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return computeBest(frame);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return ((Number) computeBest(frame)).longValue();
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) computeBest(frame)).intValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Number) computeBest(frame)).doubleValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Number) computeBest(frame)).floatValue();
    }
}
