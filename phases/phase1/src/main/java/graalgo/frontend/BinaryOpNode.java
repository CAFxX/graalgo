package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * Binary operation with exact Go semantics at every int/uint/float width,
 * plus comparisons, logical operators, and string concatenation.
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
                        || operandKind == GoKind.STRING || operandKind == GoKind.BOOL
                        || (operandKind == GoKind.INTERFACE && (op == Op.EQ || op == Op.NE)))
                    && !(operandKind == GoKind.BOOL && (op == Op.LT || op == Op.LE || op == Op.GT || op == Op.GE));
        } else if (op.isLogical()) {
            ok = kind == GoKind.BOOL && operandKind == GoKind.BOOL;
        } else if (operandKind == GoKind.STRING) {
            ok = op == Op.ADD && kind == GoKind.STRING;
        } else if (operandKind.isFloatKind()) {
            ok = (op == Op.ADD || op == Op.SUB || op == Op.MUL || op == Op.DIV) && kind == operandKind;
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
                    Panic.throwPanic(GoErrors.integerDivideByZero());
                }
                yield unsigned ? Long.divideUnsigned(l, r) : l / r;
            }
            case MOD -> {
                if (r == 0) {
                    Panic.throwPanic(GoErrors.integerDivideByZero());
                }
                yield unsigned ? Long.remainderUnsigned(l, r) : l % r;
            }
            case AND -> l & r;
            case OR -> l | r;
            case XOR -> l ^ r;
            case AND_NOT -> l & ~r;
            case SHL -> {
                if (r < 0) {
                    Panic.throwPanic(GoErrors.negativeShift());
                }
                yield r >= 64 ? 0L : l << r;
            }
            case SHR -> {
                if (r < 0) {
                    Panic.throwPanic(GoErrors.negativeShift());
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
    private static boolean interfaceEqual(Object l, Object r) {
        boolean lNil = l == null || (l instanceof graalgo.values.GoInterface li && li.isNil());
        boolean rNil = r == null || (r instanceof graalgo.values.GoInterface ri && ri.isNil());
        if (lNil || rNil) {
            return lNil && rNil;
        }
        graalgo.values.GoInterface li = (graalgo.values.GoInterface) l;
        graalgo.values.GoInterface ri = (graalgo.values.GoInterface) r;
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
        throw new UnsupportedOperationException("not an object op: " + op + " on " + operandKind);
    }
}
