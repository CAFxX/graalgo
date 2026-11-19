package graalgo.go.intrinsics;

import com.oracle.truffle.api.nodes.Node;

/**
 * Intrinsics of package {@code math}: the amd64 assembly functions (each a port of the
 * instruction sequence in upstream's {@code math/*_amd64.s}, special cases included, so that
 * results are gc's bit for bit) and the bit casts gc's compiler intrinsifies. Registered in
 * {@code internal/toolexec/intrinsics/registry.txt}.
 *
 * <p>Carriers: a {@code float32} travels as a float-representable {@code double}; a
 * {@code uint32} as a zero-extended {@code long}.
 */
final class MathIntrinsics {
    private MathIntrinsics() {}

    private static final long ABS_MASK = ~(1L << 63);
    private static final long POS_INF = 0x7FF0000000000000L;
    private static final long NEG_INF = 0xFFF0000000000000L;
    /** The NaN the assembly returns (math.NaN()). */
    private static final long NAN = 0x7FF8000000000001L;

    // floor_amd64.s: IEEE floor, ceil and truncation; NaN, ±Inf and ±0 are returned as given.
    static double math_archFloor(Node node, double x) {
        return Math.floor(x);
    }

    static double math_archCeil(Node node, double x) {
        return Math.ceil(x);
    }

    static double math_archTrunc(Node node, double x) {
        return x < 0 ? Math.ceil(x) : Math.floor(x);
    }

    // dim_amd64.s
    static double math_archMax(Node node, double x, double y) {
        long xb = Double.doubleToRawLongBits(x);
        long yb = Double.doubleToRawLongBits(y);
        if (xb == POS_INF || yb == POS_INF) {
            return Double.POSITIVE_INFINITY;
        }
        long ax = xb & ABS_MASK;
        long ay = yb & ABS_MASK;
        if (ax > POS_INF || ay > POS_INF) {
            return Double.longBitsToDouble(NAN);
        }
        if ((ax | ay) == 0) {
            return xb == (1L << 63) ? y : x;
        }
        return x > y ? x : y; // MAXSD on ordered, not both zero operands
    }

    static double math_archMin(Node node, double x, double y) {
        long xb = Double.doubleToRawLongBits(x);
        long yb = Double.doubleToRawLongBits(y);
        if (xb == NEG_INF || yb == NEG_INF) {
            return Double.NEGATIVE_INFINITY;
        }
        long ax = xb & ABS_MASK;
        long ay = yb & ABS_MASK;
        if (ax > POS_INF || ay > POS_INF) {
            return Double.longBitsToDouble(NAN);
        }
        if ((ax | ay) == 0) {
            return xb == (1L << 63) ? x : y;
        }
        return x < y ? x : y; // MINSD
    }

    // hypot_amd64.s: max * sqrt(1 + (min/max)**2)
    static double math_archHypot(Node node, double p, double q) {
        long pb = Double.doubleToRawLongBits(p) & ABS_MASK;
        long qb = Double.doubleToRawLongBits(q) & ABS_MASK;
        if (pb >= POS_INF || qb >= POS_INF) {
            return pb == POS_INF || qb == POS_INF ? Double.POSITIVE_INFINITY : Double.longBitsToDouble(NAN);
        }
        if ((pb | qb) == 0) {
            return 0;
        }
        double ap = Double.longBitsToDouble(pb);
        double aq = Double.longBitsToDouble(qb);
        double max = ap > aq ? ap : aq;
        double min = ap > aq ? aq : ap;
        double r = min / max;
        return max * Math.sqrt(1.0 + r * r);
    }

    // exp_amd64.s, the path without FMA: math.useFMA is false because internal/cpu reports no
    // optional CPU feature under graalgo (toolexec-design §5.4), i.e. gc's results under
    // GODEBUG=cpu.all=off.
    static double math_archExp(Node node, double x) {
        long xb = Double.doubleToRawLongBits(x);
        if ((xb & ABS_MASK) >= POS_INF) {
            return xb == NEG_INF ? 0 : x; // NaN or +Inf: x
        }
        if (x > 7.09782712893384e+02) {
            return Double.POSITIVE_INFINITY;
        }
        double x0 = x;
        int bx = (int) Math.rint(1.4426950408889634073599246810018920 * x0); // CVTSD2SL
        double x1 = bx;
        x0 -= 0.69314718055966295651160180568695068359375 * x1;
        x0 -= 0.28235290563031577122588448175013436025525412068e-12 * x1;
        x0 *= 0.0625;
        x1 = 2.4801587301587301587e-5;
        x1 = x1 * x0 + 1.9841269841269841270e-4;
        x1 = x1 * x0 + 1.3888888888888888889e-3;
        x1 = x1 * x0 + 8.3333333333333333333e-3;
        x1 = x1 * x0 + 4.1666666666666666667e-2;
        x1 = x1 * x0 + 1.6666666666666666667e-1;
        x1 = x1 * x0 + 0.5;
        x1 = x1 * x0 + 1.0;
        x0 = x0 * x1;
        for (int i = 0; i < 4; i++) {
            x1 = 2.0 + x0;
            x0 = x0 * x1;
        }
        x0 += 1.0;
        // fr * 2**exponent
        bx += 0x3FF;
        if (bx <= 0) {
            if (bx < -52) {
                return 0;
            }
            x0 *= Double.longBitsToDouble((long) (bx + 0x3FE) << 52);
            bx = 1;
        } else if (bx >= 0x7FF) {
            return Double.POSITIVE_INFINITY;
        }
        return x0 * Double.longBitsToDouble((long) bx << 52);
    }

    // log_amd64.s
    static double math_archLog(Node node, double x) {
        long xb = Double.doubleToRawLongBits(x);
        if ((xb & ABS_MASK) == 0) {
            return Double.NEGATIVE_INFINITY;
        }
        if (xb < 0) {
            return Double.longBitsToDouble(NAN);
        }
        if (xb >= POS_INF) {
            return x;
        }
        // f1, ki := math.Frexp(x) on the bits (a subnormal is not normalized, as upstream).
        double f1 = Double.longBitsToDouble((xb & 0x000FFFFFFFFFFFFFL) | 0x3FE0000000000000L);
        double k = (int) ((xb >>> 52) & 0x7FF) - 0x3FE;
        double adj = !(7.07106781186547524401e-01 < f1) ? 1.0 : 0.0; // CMPSD, not-less-than
        k -= adj;
        f1 *= adj + 1.0;
        double f = f1 - 1.0;
        double s = f / (2.0 + f);
        double s2 = s * s;
        double s4 = s2 * s2;
        double t1 = s2 * (((1.479819860511658591e-01 * s4 + 1.818357216161805012e-01) * s4
                + 2.857142874366239149e-01) * s4 + 6.666666666666735130e-01);
        double t2 = s4 * ((1.531383769920937332e-01 * s4 + 2.222219843214978396e-01) * s4
                + 3.999999999940941908e-01);
        double r = t1 + t2;
        double hfsq = 0.5 * f * f;
        double t = s * (r + hfsq) + 1.90821492927058770002e-10 * k;
        return k * 6.93147180369123816490e-01 - ((hfsq - t) - f);
    }

    // Bit casts (gc: a register move). A float32 signalling NaN does not survive the double
    // carrier bit for bit (truffle-design §15, carriers).
    static long math_Float64bits(Node node, double f) {
        return Double.doubleToRawLongBits(f);
    }

    static double math_Float64frombits(Node node, long b) {
        return Double.longBitsToDouble(b);
    }

    static long math_Float32bits(Node node, double f) {
        return Float.floatToRawIntBits((float) f) & 0xFFFFFFFFL;
    }

    static double math_Float32frombits(Node node, long b) {
        return Float.intBitsToFloat((int) b);
    }

    // math.sqrt: gc emits SQRTSD; Math.sqrt is the same correctly rounded IEEE square root.
    static double math_sqrt(Node node, double x) {
        return Math.sqrt(x);
    }
}
