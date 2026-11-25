package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * A typed constant.
 *
 * <p>Integer constants carry the low 64 bits ({@link NodeFactory#parseGoIntConst});
 * go/types guarantees representability in the use-site type, so truncation/
 * extension is exact. Float constants carry the correctly-rounded IEEE bits
 * for both widths ({@code f32bits}/{@code f64bits} from the exporter, computed
 * with go/constant itself) — no decimal parsing, no double rounding.
 * Untyped integer constants additionally keep their exact decimal string so
 * float conversions ({@code float64(1<<70)}) and rune conversions
 * ({@code string(1<<70)}) are exact even beyond 64 bits.
 */
public final class ConstNode extends GoExprNode {
    private final long longValue;
    private final float floatValue;
    private final double doubleValue;
    private final boolean boolValue;
    private final GoString stringValue;
    private final Object objectValue;
    /** Exact decimal for untyped int/float constants; null otherwise. */
    private final String decimal;
    private final int f32bits;
    private final long f64bits;

    private ConstNode(GoKind kind, long longValue, float floatValue, double doubleValue,
                      boolean boolValue, GoString stringValue, Object objectValue,
                      String decimal, int f32bits, long f64bits) {
        super(kind);
        this.longValue = longValue;
        this.floatValue = floatValue;
        this.doubleValue = doubleValue;
        this.boolValue = boolValue;
        this.stringValue = stringValue;
        this.objectValue = objectValue;
        this.decimal = decimal;
        this.f32bits = f32bits;
        this.f64bits = f64bits;
    }

    /** Integer/uint constant: {@code bits} are the low 64 bits. */
    public static ConstNode ofInt(GoKind kind, long bits) {
        return new ConstNode(kind, bits, 0f, 0d, false, null, null, null, 0, 0L);
    }

    /** Untyped integer constant: {@code decimal} is the exact decimal string. */
    public static ConstNode ofUntypedInt(long bits, String decimal) {
        return new ConstNode(GoKind.UNTYPED_INT, bits, 0f, 0d, false, null, null, decimal, 0, 0L);
    }

    /** Float constant from the exporter's correctly-rounded bits. */
    public static ConstNode ofFloat(GoKind kind, int f32bits, long f64bits) {
        return new ConstNode(kind, 0L,
                Float.intBitsToFloat(f32bits), Double.longBitsToDouble(f64bits),
                false, null, null, null, f32bits, f64bits);
    }

    /** Untyped float constant: correctly-rounded bits for both widths. */
    public static ConstNode ofUntypedFloat(int f32bits, long f64bits) {
        return new ConstNode(GoKind.UNTYPED_FLOAT, 0L, 0f, 0d, false, null, null, null, f32bits, f64bits);
    }

    public static ConstNode ofBool(boolean value) {
        return new ConstNode(GoKind.BOOL, 0L, 0f, 0d, value, null, null, null, 0, 0L);
    }

    public static ConstNode ofUntypedBool(boolean value) {
        return new ConstNode(GoKind.UNTYPED_BOOL, 0L, 0f, 0d, value, null, null, null, 0, 0L);
    }

    public static ConstNode ofString(GoString value) {
        return new ConstNode(GoKind.STRING, 0L, 0f, 0d, false, value, null, null, 0, 0L);
    }

    public static ConstNode ofUntypedString(GoString value) {
        return new ConstNode(GoKind.UNTYPED_STRING, 0L, 0f, 0d, false, value, null, null, 0, 0L);
    }

    /** Reference-kind constant (nil or zero struct). */
    public static ConstNode ofObject(GoKind kind, Object value) {
        return new ConstNode(kind, 0L, 0f, 0d, false, null, value, null, 0, 0L);
    }

    /** Exact decimal for untyped int/float constants; null otherwise. */
    @Override
    public String exactDecimal() {
        return decimal;
    }

    private static int extendInt(long bits, GoKind kind) {
        return switch (kind) {
            case INT8 -> (int) (byte) bits;
            case INT16 -> (int) (short) bits;
            case UINT8 -> (int) bits & 0xFF;
            case UINT16 -> (int) bits & 0xFFFF;
            default -> (int) bits;
        };
    }

    private static long extendLong(long bits, GoKind kind) {
        return switch (kind) {
            case INT8 -> (long) (byte) bits;
            case INT16 -> (long) (short) bits;
            case INT32 -> (long) (int) bits;
            default -> bits;
        };
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return extendLong(longValue, kind());
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return extendInt(longValue, kind());
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return switch (kind()) {
            case FLOAT32 -> floatValue;
            case UNTYPED_FLOAT -> Float.intBitsToFloat(f32bits);
            case UNTYPED_INT -> Float.parseFloat(decimal);
            default -> throw new UnsupportedOperationException("const " + kind() + " has no float value");
        };
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return switch (kind()) {
            case FLOAT64 -> doubleValue;
            case FLOAT32 -> (double) floatValue;
            case UNTYPED_FLOAT -> Double.longBitsToDouble(f64bits);
            case UNTYPED_INT -> Double.parseDouble(decimal);
            default -> throw new UnsupportedOperationException("const " + kind() + " has no double value");
        };
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return boolValue;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (kind() == GoKind.STRING || kind() == GoKind.UNTYPED_STRING) {
            return stringValue;
        }
        return objectValue;
    }
}
