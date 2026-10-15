package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * Binary operation with exact Go semantics at every int/uint/float width,
 * plus complex arithmetic, comparisons, logical operators, and string
 * concatenation.
 *
 * <p>The node's {@link GoKind} is the <em>result</em> kind (BOOL for
 * comparisons and {@code &&}/{@code ||}); {@code operandKind} is the kind
 * the operation works in (identical for arithmetic). Go-exact rules
 * (all verified against stock gc, not just Java):
 *
 * <ul>
 *   <li>Signed {@code /} and {@code %} truncate toward zero with the
 *       dividend's sign — identical to Java; unsigned uses
 *       {@link Long#divideUnsigned}/{@link Long#remainderUnsigned}.
 *       Division by zero throws {@link ArithmeticException}, the M2
 *       stand-in for Go's runtime panic (WS F maps it).</li>
 *   <li>Sub-64-bit arithmetic is computed on the extended (sign- or
 *       zero-extended, per signedness) {@code long} value and truncated to
 *       the width: wraparound, {@code minint/-1} (no trap), and bitwise
 *       ops are all exact.</li>
 *   <li>Shifts implement Go's rule, never Java's count masking: negative
 *       count panics; count ≥ 64 yields 0 for {@code <<} and unsigned
 *       {@code >>}, and sign extension for signed {@code >>}; smaller
 *       counts shift the extended value and truncate to the width.</li>
 *   <li>Float arithmetic is IEEE 754 (matches Go); comparisons follow
 *       IEEE NaN semantics on both sides.</li>
 *   <li>Complex arithmetic ({@code + - * /} on {@code complex64}/
 *       {@code complex128}, PKG-026): values travel as
 *       {@link GoComplex32}/{@link GoComplex64} objects.
 *       Bit-exact gc semantics, read off the gc sources
 *       ({@code cmd/compile/internal/ssagen/ssa.go},
 *       {@code runtime/complex.go}): {@code +}/{@code -} component-wise
 *       in the component precision; {@code *} widens {@code complex64}
 *       to {@code float64}, computes {@code (ac-bd)+(ad+bc)i}, narrows
 *       back; {@code /} widens to {@code complex128} and calls the
 *       Smith-1962 robust division ({@code runtime.complex128div},
 *       with the C99 edge-case corrections), narrowing back for
 *       {@code complex64}. Complex division never panics.</li>
 *   <li>String {@code +} concatenates; string comparisons are unsigned
 *       lexicographic on bytes ({@link GoString#compareTo}).</li>
 *   <li>{@code &&} and {@code ||} short-circuit (Java's {@code &&}/
 *       {@code ||} on the boolean operands).</li>
 * </ul>
 */
public final class BinaryOpNode extends GoExprNode {
    public enum Op {
        ADD, SUB, MUL, DIV, MOD, AND, OR, XOR, AND_NOT, SHL, SHR,
        EQ, NE, LT, LE, GT, GE, LAND, LOR;

        public static Op fromToken(String tok) {
            return switch (tok) {
                case "+" -> ADD;
                case "-" -> SUB;
                case "*" -> MUL;
                case "/" -> DIV;
                case "%" -> MOD;
                case "&" -> AND;
                case "|" -> OR;
                case "^" -> XOR;
                case "&^" -> AND_NOT;
                case "<<" -> SHL;
                case ">>" -> SHR;
                case "==" -> EQ;
                case "!=" -> NE;
                case "<" -> LT;
                case "<=" -> LE;
                case ">" -> GT;
                case ">=" -> GE;
                case "&&" -> LAND;
                case "||" -> LOR;
                default -> throw new FrontendException(
                        "unsupported binary operator \"" + tok + "\" (WS D hook: registerExprKind(\"binary\"))");
            };
        }

        /** Maps an assign-op token ("+=") to its binary op; "=" maps to null. */
        public static Op fromAssignToken(String tok) {
            if ("=".equals(tok)) {
                return null;
            }
            if (tok.endsWith("=")) {
                return fromToken(tok.substring(0, tok.length() - 1));
            }
            throw new FrontendException("bad assign token \"" + tok + "\"");
        }

        boolean isComparison() {
            return switch (this) {
                case EQ, NE, LT, LE, GT, GE -> true;
                default -> false;
            };
        }

        boolean isLogical() {
            return this == LAND || this == LOR;
        }

        boolean isShift() {
            return this == SHL || this == SHR;
        }
    }

    @Child private GoExprNode left;
    @Child private GoExprNode right;
    private final Op op;
    /** The kind the operation works in (== kind() for arithmetic). */
    private final GoKind operandKind;

    /**
     * @param kind the result kind (BOOL for comparisons/logical ops)
     * @param operandKind the kind the operation works in
     */
    public BinaryOpNode(Op op, GoKind kind, GoKind operandKind, GoExprNode left, GoExprNode right) {
        super(kind);
        checkCombo(op, kind, operandKind);
        this.op = op;
        this.operandKind = operandKind;
        this.left = left;
        this.right = right;
    }

    /** Arithmetic-only constructor: result kind == operand kind. */
    public BinaryOpNode(Op op, GoKind kind, GoExprNode left, GoExprNode right) {
        this(op, kind, kind, left, right);
    }

    private static void checkCombo(Op op, GoKind kind, GoKind operandKind) {
        boolean ok;
        if (op.isComparison()) {
            ok = kind == GoKind.BOOL
                    && (operandKind.isIntKind() || operandKind.isFloatKind()
                        || operandKind.isComplexKind()
                        || operandKind == GoKind.STRING || operandKind == GoKind.BOOL
                        || operandKind == GoKind.UNSAFE_POINTER
                        || operandKind == GoKind.POINTER
                        || (operandKind == GoKind.INTERFACE && (op == Op.EQ || op == Op.NE))
                        || (operandKind == GoKind.STRUCT && (op == Op.EQ || op == Op.NE))
                        || (operandKind == GoKind.ARRAY && (op == Op.EQ || op == Op.NE)))
                    && !(operandKind == GoKind.BOOL && (op == Op.LT || op == Op.LE || op == Op.GT || op == Op.GE));
            // P3-M8b: unsafe.Pointer, *T pointers, and complex support ==/!=
            // (but not ordering).
            // P3-M9 (tier-1d): structs support ==/!= (but not ordering).
            // P3-M9 (arrdispatch): arrays support ==/!= (but not ordering) —
            // Go arrays are comparable iff the element type is (go/types
            // enforces this; the backend compares element-wise).
            if ((operandKind == GoKind.UNSAFE_POINTER || operandKind == GoKind.POINTER
                    || operandKind.isComplexKind() || operandKind == GoKind.STRUCT
                    || operandKind == GoKind.ARRAY)
                    && !(op == Op.EQ || op == Op.NE)) {
                ok = false;
            }
        } else if (op.isLogical()) {
            ok = kind == GoKind.BOOL && operandKind == GoKind.BOOL;
        } else if (operandKind == GoKind.STRING) {
            ok = op == Op.ADD && kind == GoKind.STRING;
        } else if (operandKind.isFloatKind()) {
            ok = (op == Op.ADD || op == Op.SUB || op == Op.MUL || op == Op.DIV) && kind == operandKind;
        } else if (operandKind.isComplexKind()) {
            // PKG-026: the complex arithmetic matrix. Go allows + - * /
            // on complex64/complex128 (both operands already converted to
            // the result kind by the factory); % & | ^ &^ << >> are type
            // errors — go/types rejects them before the backend.
            ok = (op == Op.ADD || op == Op.SUB || op == Op.MUL || op == Op.DIV)
                    && kind == operandKind;
        } else if (operandKind.isIntKind()) {
            ok = kind == operandKind;
        } else {
            ok = false;
        }
        if (!ok) {
            throw new IllegalArgumentException(
                    "illegal binary op/kind combination: " + op + " result=" + kind + " operand=" + operandKind);
        }
    }

    private static long mask(long v, int width) {
        return switch (width) {
            case 8 -> v & 0xFFL;
            case 16 -> v & 0xFFFFL;
            case 32 -> v & 0xFFFFFFFFL;
            default -> v;
        };
    }

    private static long extend(long masked, GoKind kind) {
        return switch (kind) {
            case INT8 -> (long) (byte) masked;
            case INT16 -> (long) (short) masked;
            case INT32 -> (long) (int) masked;
            default -> masked; // 64-bit kinds and unsigned: already the value
        };
    }

    /**
     * Re-extends masked low-width bits to the int carrier: signed kinds
     * sign-extend, unsigned kinds zero-extend (D5: the int carrier always
     * holds the extended value, never raw bits).
     */
    private static int extendInt(int masked, GoKind kind) {
        return switch (kind) {
            case INT8 -> (int) (byte) masked;
            case INT16 -> (int) (short) masked;
            case UINT8 -> masked & 0xFF;
            case UINT16 -> masked & 0xFFFF;
            default -> masked; // INT32/UINT32: the int is the value
        };
    }

    /** Integer arithmetic result as masked (low-width) bits. */
    private long intArith(long l, long r) {
        int width = operandKind.bits();
        boolean unsigned = operandKind.isUintKind();
        long raw = switch (op) {
            case ADD -> l + r;
            case SUB -> l - r;
            case MUL -> l * r;
            case DIV -> {
                if (r == 0) {
                    Panic.throwPanic(GoErrors.integerDivideByZero(), null, null, this);
                }
                yield unsigned ? Long.divideUnsigned(l, r) : l / r;
            }
            case MOD -> {
                if (r == 0) {
                    Panic.throwPanic(GoErrors.integerDivideByZero(), null, null, this);
                }
                yield unsigned ? Long.remainderUnsigned(l, r) : l % r;
            }
            case AND -> l & r;
            case OR -> l | r;
            case XOR -> l ^ r;
            case AND_NOT -> l & ~r;
            case SHL -> {
                if (r < 0) {
                    Panic.throwPanic(GoErrors.negativeShift(), null, null, this);
                }
                yield r >= 64 ? 0L : l << r;
            }
            case SHR -> {
                if (r < 0) {
                    Panic.throwPanic(GoErrors.negativeShift(), null, null, this);
                }
                if (r >= 64) {
                    yield unsigned ? 0L : (l < 0 ? -1L : 0L);
                }
                yield unsigned ? l >>> r : l >> r;
            }
            default -> throw new UnsupportedOperationException("not an integer op: " + op);
        };
        return mask(raw, width);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long l = left.executeLong(frame);
        long r = right.executeLong(frame);
        return extend(intArith(l, r), operandKind);
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return extendInt((int) intArith(left.executeLong(frame), right.executeLong(frame)), operandKind);
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        float l = left.executeFloat(frame);
        float r = right.executeFloat(frame);
        return switch (op) {
            case ADD -> l + r;
            case SUB -> l - r;
            case MUL -> l * r;
            case DIV -> l / r;
            default -> throw new UnsupportedOperationException("not a float op: " + op);
        };
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        // bug-010: Go rounds every float32 operation result to float32
        // precision. Computing in double and widening skips that rounding,
        // so a directly-consumed float32 add/mul/sub/div (e.g. in a
        // comparison) sees the unrounded double value. Route through the
        // float path (correctly rounded) and widen exactly.
        if (operandKind == GoKind.FLOAT32) {
            return executeFloat(frame);
        }
        double l = left.executeDouble(frame);
        double r = right.executeDouble(frame);
        return switch (op) {
            case ADD -> l + r;
            case SUB -> l - r;
            case MUL -> l * r;
            case DIV -> l / r;
            default -> throw new UnsupportedOperationException("not a float op: " + op);
        };
    }

    /** Interface equality: nil == nil; else same descriptor and equal data. */
    private boolean interfaceEqual(Object l, Object r) {
        boolean lNil = l == null || (l instanceof graalgo.values.GoInterface li && li.isNil());
        boolean rNil = r == null || (r instanceof graalgo.values.GoInterface ri && ri.isNil());
        if (lNil || rNil) {
            return lNil && rNil;
        }
        graalgo.values.GoInterface li = (graalgo.values.GoInterface) l;
        graalgo.values.GoInterface ri = (graalgo.values.GoInterface) r;
        // bug-019: gc panics ("comparing uncomparable type") when both sides
        // have the identical dynamic type and that type is uncomparable
        // (func, slice, map, or struct/array containing one). Different
        // dynamic types, or a nil side, never panic.
        if (li.descriptor() == ri.descriptor()) {
            graalgo.values.GoTypeDescriptor td = (graalgo.values.GoTypeDescriptor) li.descriptor();
            if (!td.isComparable()) {
                Panic.throwPanic(GoErrors.comparingUncomparable(td.name()), null, null, this);
            }
        }
        return li.descriptor() == ri.descriptor()
                && graalgo.values.GoEquality.equal(li.data(), ri.data());
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        if (op.isLogical()) {
            return switch (op) {
                case LAND -> left.executeBoolean(frame) && right.executeBoolean(frame);
                case LOR -> left.executeBoolean(frame) || right.executeBoolean(frame);
                default -> throw new AssertionError("unreachable");
            };
        }
        if (!op.isComparison()) {
            throw new UnsupportedOperationException("not a boolean op: " + op);
        }
        // M5 (WS F): interface equality (== nil, != nil). A nil interface
        // (Java null or GoInterface with null descriptor) equals only nil.
        // Otherwise, descriptors must be identical and data must be equal.
        if (operandKind == GoKind.INTERFACE) {
            Object l = left.executeObject(frame);
            Object r = right.executeObject(frame);
            boolean eq = interfaceEqual(l, r);
            return op == Op.EQ ? eq : !eq;
        }
        int cmp;
        // M8 closeout: float comparisons use IEEE 754 semantics (Java's
        // primitive operators), not Double.compare's total ordering.
        // Double.compare distinguishes -0.0 from +0.0 and orders NaN above
        // everything; Go's ==/</<= all follow IEEE (-0.0 == 0.0 is true,
        // NaN comparisons are false).
        if (operandKind == GoKind.FLOAT32 || operandKind == GoKind.FLOAT64
                || operandKind == GoKind.UNTYPED_FLOAT) {
            double l = left.executeDouble(frame);
            double r = right.executeDouble(frame);
            return switch (op) {
                case EQ -> l == r;
                case NE -> l != r;
                case LT -> l < r;
                case LE -> l <= r;
                case GT -> l > r;
                case GE -> l >= r;
                default -> throw new AssertionError("unreachable");
            };
        }
        // P3-M8b: *T pointer equality (Go: ==/!= on pointers compares
        // the addressed storage, not the values stored there).
        if (operandKind == GoKind.POINTER) {
            Object l = left.executeObject(frame);
            Object r = right.executeObject(frame);
            // Nil pointers arrive as Java null or as a nil GoPointer;
            // normalize the latter to null so the two compare equal.
            Object ln = l instanceof graalgo.values.GoPointer lgp && lgp.isNil() ? null : l;
            Object rn = r instanceof graalgo.values.GoPointer rgp && rgp.isNil() ? null : r;
            boolean eq;
            if (ln == null || rn == null) {
                eq = ln == null && rn == null;
            } else if (ln instanceof graalgo.values.GoPointer lp
                    && rn instanceof graalgo.values.GoPointer rp) {
                eq = lp.sameStorageAs(rp);
            } else {
                eq = ln == rn;
            }
            return op == Op.EQ ? eq : !eq;
        }
        // P3-M8b: complex equality (Go: ==/!= on complex compares both parts).
        if (operandKind.isComplexKind()) {
            Object l = left.executeObject(frame);
            Object r = right.executeObject(frame);
            boolean eq;
            if (l instanceof graalgo.values.GoComplex64 lc && r instanceof graalgo.values.GoComplex64 rc) {
                eq = lc.re() == rc.re() && lc.im() == rc.im();
            } else if (l instanceof graalgo.values.GoComplex32 lc32
                    && r instanceof graalgo.values.GoComplex32 rc32) {
                eq = lc32.re() == rc32.re() && lc32.im() == rc32.im();
            } else {
                throw new FrontendException("complex comparison of mismatched types");
            }
            return op == Op.EQ ? eq : !eq;
        }
        // P3-M9 (tier-1d): struct equality (Go: ==/!= on comparable structs
        // compares field-by-field; GoEquality delegates to GoStruct.equals,
        // which requires identical descriptors and compares longs by value
        // and objects via GoEquality).
        if (operandKind == GoKind.STRUCT) {
            Object l = left.executeObject(frame);
            Object r = right.executeObject(frame);
            boolean eq = graalgo.values.GoEquality.equal(l, r);
            return op == Op.EQ ? eq : !eq;
        }
        // P3-M9 (arrdispatch): array equality (Go: ==/!= on comparable
        // arrays compares element-wise; go/types rejects uncomparable
        // element types before the backend, so every ARRAY reaching here
        // is comparable. GoEquality handles each carrier form with IEEE
        // float semantics, mirroring the STRUCT branch above).
        if (operandKind == GoKind.ARRAY) {
            Object l = left.executeObject(frame);
            Object r = right.executeObject(frame);
            boolean eq = graalgo.values.GoEquality.equal(l, r);
            return op == Op.EQ ? eq : !eq;
        }
        cmp = switch (operandKind) {
            case STRING -> ((GoString) left.executeObject(frame))
                    .compareTo((GoString) right.executeObject(frame));
            case BOOL -> Boolean.compare(left.executeBoolean(frame), right.executeBoolean(frame));
            default -> {
                long l = left.executeLong(frame);
                long r = right.executeLong(frame);
                yield operandKind.isUintKind() ? Long.compareUnsigned(l, r) : Long.compare(l, r);
            }
        };
        return switch (op) {
            case EQ -> cmp == 0;
            case NE -> cmp != 0;
            case LT -> cmp < 0;
            case LE -> cmp <= 0;
            case GT -> cmp > 0;
            case GE -> cmp >= 0;
            default -> throw new AssertionError("unreachable");
        };
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (op == Op.ADD && operandKind == GoKind.STRING) {
            return ((GoString) left.executeObject(frame)).concat((GoString) right.executeObject(frame));
        }
        // PKG-026: complex arithmetic (checkCombo admits only ADD/SUB/MUL/DIV here).
        if (operandKind.isComplexKind()) {
            return complexArith(frame);
        }
        throw new UnsupportedOperationException("not an object op: " + op + " on " + operandKind);
    }

    /**
     * Complex {@code + - * /} (PKG-026). Operands arrive as
     * {@link GoComplex32} or {@link GoComplex64} — an untyped-complex
     * constant is transparent to complex targets (see
     * {@link GoKind#transparent}) and keeps its {@code GoComplex64}
     * carrier, so components are read generically.
     *
     * <p>Semantics are gc's, read off the gc sources
     * ({@code cmd/compile/internal/ssagen/ssa.go} §OMUL/OADD/OSUB and
     * {@code runtime/complex.go} §complex128div), not the naive
     * formulas: {@code +}/{@code -} are component-wise in the component
     * precision; {@code *} widens {@code complex64} operands to
     * {@code float64} ("minimize cancellation error"), computes
     * {@code (ac-bd)+(ad+bc)i}, and narrows each component back;
     * {@code /} widens both operands to {@code complex128} and applies
     * {@link #complex128div} (narrowing back for {@code complex64}).
     */
    private Object complexArith(VirtualFrame frame) {
        if (operandKind == GoKind.COMPLEX64) {
            float[] l = complexPartsF(left.executeObject(frame));
            float[] r = complexPartsF(right.executeObject(frame));
            float re, im;
            switch (op) {
                case ADD -> { re = l[0] + r[0]; im = l[1] + r[1]; }
                case SUB -> { re = l[0] - r[0]; im = l[1] - r[1]; }
                case MUL -> {
                    // gc widens to float64 for the multiply, then narrows.
                    double ar = l[0], ai = l[1], br = r[0], bi = r[1];
                    re = (float) (ar * br - ai * bi);
                    im = (float) (ar * bi + ai * br);
                }
                case DIV -> {
                    // gc: widen to complex128, runtime.complex128div, narrow.
                    double[] q = complex128div(l[0], l[1], r[0], r[1]);
                    re = (float) q[0];
                    im = (float) q[1];
                }
                default -> throw new UnsupportedOperationException("not a complex op: " + op);
            }
            return new GoComplex32(re, im);
        }
        double[] l = complexParts(left.executeObject(frame));
        double[] r = complexParts(right.executeObject(frame));
        double re, im;
        switch (op) {
            case ADD -> { re = l[0] + r[0]; im = l[1] + r[1]; }
            case SUB -> { re = l[0] - r[0]; im = l[1] - r[1]; }
            case MUL -> { re = l[0] * r[0] - l[1] * r[1]; im = l[0] * r[1] + l[1] * r[0]; }
            case DIV -> {
                double[] q = complex128div(l[0], l[1], r[0], r[1]);
                re = q[0];
                im = q[1];
            }
            default -> throw new UnsupportedOperationException("not a complex op: " + op);
        }
        return new GoComplex64(re, im);
    }

    /**
     * Faithful port of gc's {@code runtime.complex128div}
     * ({@code src/runtime/complex.go}): Robert L. Smith's Algorithm 116
     * (1962) robust complex division, with the C99 G.5.1 edge-case
     * corrections for NaN/Infinity operands. gc rewrites <em>every</em>
     * complex division (including {@code complex64}, after widening)
     * into a call to this function, so bit-exactness here is what makes
     * graalgo's complex division match gc's.
     *
     * @param ar real(n), @param ai imag(n), @param br real(m), @param bi imag(m)
     * @return {@code [re, im]} of {@code n/m}
     */
    static double[] complex128div(double ar, double ai, double br, double bi) {
        double e, f;
        if (Math.abs(br) >= Math.abs(bi)) {
            double ratio = bi / br;
            double denom = br + ratio * bi;
            e = (ar + ai * ratio) / denom;
            f = (ai - ar * ratio) / denom;
        } else {
            double ratio = br / bi;
            double denom = bi + ratio * br;
            e = (ar * ratio + ai) / denom;
            f = (ai * ratio - ar) / denom;
        }
        if (Double.isNaN(e) && Double.isNaN(f)) {
            // Correct NaN results to infinities/zeros where C99 mandates it.
            if (br == 0.0 && bi == 0.0 && (!Double.isNaN(ar) || !Double.isNaN(ai))) {
                // m == 0 (Go complex equality: signed zeros compare equal).
                double s = Math.copySign(Double.POSITIVE_INFINITY, br);
                e = s * ar;
                f = s * ai;
            } else if ((Double.isInfinite(ar) || Double.isInfinite(ai))
                    && isFinite(br) && isFinite(bi)) {
                double a = inf2one(ar);
                double b = inf2one(ai);
                e = Double.POSITIVE_INFINITY * (a * br + b * bi);
                f = Double.POSITIVE_INFINITY * (b * br - a * bi);
            } else if ((Double.isInfinite(br) || Double.isInfinite(bi))
                    && isFinite(ar) && isFinite(ai)) {
                double c = inf2one(br);
                double d = inf2one(bi);
                e = 0.0 * (ar * c + ai * d);
                f = 0.0 * (ai * c - ar * d);
            }
        }
        return new double[] { e, f };
    }

    /** gc's {@code inf2one}: signed 1 for infinities, signed 0 otherwise. */
    private static double inf2one(double x) {
        double g = 0.0;
        if (Double.isInfinite(x)) {
            g = 1.0;
        }
        return Math.copySign(g, x);
    }

    /** gc's {@code isFinite}: neither NaN nor infinite. */
    private static boolean isFinite(double x) {
        return !Double.isNaN(x) && !Double.isInfinite(x);
    }

    /** {@code [re, im]} as doubles from either complex carrier. */
    private static double[] complexParts(Object v) {
        if (v instanceof GoComplex64 c) {
            return new double[] { c.re(), c.im() };
        }
        if (v instanceof GoComplex32 c) {
            return new double[] { c.re(), c.im() };
        }
        throw new FrontendException("complex operand is not a complex value: " + v + " (PKG-026)");
    }

    /** {@code [re, im]} as floats from either complex carrier. */
    private static float[] complexPartsF(Object v) {
        if (v instanceof GoComplex32 c) {
            return new float[] { c.re(), c.im() };
        }
        if (v instanceof GoComplex64 c) {
            return new float[] { (float) c.re(), (float) c.im() };
        }
        throw new FrontendException("complex operand is not a complex value: " + v + " (PKG-026)");
    }
}
