package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * Unary operation with exact Go semantics: {@code -x}, {@code ^x} on
 * int/uint kinds (truncated to the width), {@code -x} on floats,
 * {@code !x} on bools, {@code +x} (identity).
 *
 * <p>Go restricts {@code !} to booleans and {@code ^} to integers
 * (go/types enforces this); the constructor rejects anything else loud.
 */
public final class UnaryOpNode extends GoExprNode {
    public enum Op {
        NEG, NOT, LOGNOT, POS;

        public static Op fromToken(String tok) {
            return switch (tok) {
                case "-" -> NEG;
                case "^" -> NOT;
                case "!" -> LOGNOT;
                case "+" -> POS;
                default -> throw new FrontendException(
                        "unsupported unary operator \"" + tok + "\" (WS D hook: registerExprKind(\"unary\"))");
            };
        }
    }

    @Child private GoExprNode child;
    private final Op op;

    public UnaryOpNode(Op op, GoKind kind, GoExprNode child) {
        super(kind);
        boolean ok = switch (op) {
            case NEG -> kind.isIntKind() || kind.isFloatKind();
            case NOT -> kind.isIntKind();
            case LOGNOT -> kind == GoKind.BOOL;
            case POS -> kind.isIntKind() || kind.isFloatKind();
        };
        if (!ok) {
            throw new IllegalArgumentException("illegal unary op/kind combination: " + op + " on " + kind);
        }
        this.op = op;
        this.child = child;
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
            default -> masked;
        };
    }

    private static int extendInt(int masked, GoKind kind) {
        return switch (kind) {
            case INT8 -> (int) (byte) masked;
            case INT16 -> (int) (short) masked;
            case UINT8 -> masked & 0xFF;
            case UINT16 -> masked & 0xFFFF;
            default -> masked;
        };
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long x = child.executeLong(frame);
        long raw = switch (op) {
            case NEG -> -x;
            case NOT -> ~x;
            case POS -> x;
            default -> throw new UnsupportedOperationException("not an integer unary op: " + op);
        };
        return extend(mask(raw, kind().bits()), kind());
    }

    /**
     * Exact decimal through an untyped-constant operand. Only {@code -},
     * {@code ^}, {@code +} on untyped ints can reach here (the exporter
     * never leaves other untyped-unary shapes unconverted); everything else
     * throws.
     */
    @Override
    public String exactDecimal() {
        if (kind() != GoKind.UNTYPED_INT) {
            return super.exactDecimal();
        }
        java.math.BigInteger x = new java.math.BigInteger(child.exactDecimal());
        return switch (op) {
            case NEG -> x.negate().toString();
            case NOT -> x.not().toString(); // BigInteger.not() == -x-1 == Go ^x
            case POS -> x.toString();
            default -> throw new FrontendException(
                    "exactDecimal() is not defined for unary op " + op + " (WS D)");
        };
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        int x = child.executeInt(frame);
        int raw = switch (op) {
            case NEG -> -x;
            case NOT -> ~x;
            case POS -> x;
            default -> throw new UnsupportedOperationException("not an integer unary op: " + op);
        };
        return extendInt(raw, kind());
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        float x = child.executeFloat(frame);
        return switch (op) {
            case NEG -> -x;
            case POS -> x;
            default -> throw new UnsupportedOperationException("not a float unary op: " + op);
        };
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        double x = child.executeDouble(frame);
        return switch (op) {
            case NEG -> -x;
            case POS -> x;
            default -> throw new UnsupportedOperationException("not a float unary op: " + op);
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        if (op == Op.LOGNOT) {
            return !child.executeBoolean(frame);
        }
        throw new UnsupportedOperationException("not a boolean unary op: " + op);
    }
}
