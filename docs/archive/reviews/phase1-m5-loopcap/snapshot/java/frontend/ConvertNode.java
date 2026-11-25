package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoConvert;
import graalgo.values.GoKind;

/**
 * A Go conversion {@code T(x)} (frontend {@code convert} node, M3 WS D).
 *
 * <p>The full conversion matrix with Go-exact semantics (verified against
 * stock gc):
 * <ul>
 *   <li>int/uint ↔ int/uint: bit truncation / sign- or zero-extension.</li>
 *   <li>int/uint → float32/64: correctly rounded (Java casts are; uint64
 *       uses {@link GoConvert#uint64ToDouble}/{@link GoConvert#uint64ToFloat}).</li>
 *   <li>float → int/uint: truncation toward zero; out-of-range mirrors
 *       gc/amd64 ({@link GoConvert}).</li>
 *   <li>float32 ↔ float64: widening exact, narrowing correctly rounded.</li>
 *   <li>int → string: the rune conversion (out-of-range → U+FFFD).</li>
 * </ul>
 * Untyped constants never reach this node for same-family targets (they
 * are transparent — {@link GoKind#transparent}); the one untyped source
 * that does ({@code string(65)}) uses the constant's exact decimal.
 */
public final class ConvertNode extends GoExprNode {
    private final GoKind from;
    @Child private GoExprNode child;

    public ConvertNode(GoKind from, GoKind to, GoExprNode child) {
        super(to);
        checkCombo(from, to);
        this.from = from;
        this.child = child;
    }

    private static void checkCombo(GoKind from, GoKind to) {
        boolean numeric = (from.isIntKind() || from.isFloatKind())
                && (to.isIntKind() || to.isFloatKind());
        boolean intToString = from.isIntKind() && to == GoKind.STRING;
        boolean stringToString = from == GoKind.STRING && to == GoKind.STRING;
        boolean boolToBool = from == GoKind.BOOL && to == GoKind.BOOL;
        boolean unsafeEscape = from == GoKind.UNSAFE_POINTER && to == GoKind.UINT64;
        if (!(numeric || intToString || stringToString || boolToBool || unsafeEscape)) {
            throw new IllegalArgumentException(
                    "illegal conversion: " + from + " -> " + to);
        }
    }

    private static int extendInt(long bits, GoKind to) {
        int v = (int) bits;
        return switch (to) {
            case INT8 -> (int) (byte) v;
            case INT16 -> (int) (short) v;
            case UINT8 -> v & 0xFF;
            case UINT16 -> v & 0xFFFF;
            default -> v;
        };
    }

    private static long extendLong(long bits, GoKind to) {
        return switch (to) {
            case INT8 -> (long) (byte) bits;
            case INT16 -> (long) (short) bits;
            case INT32 -> (long) (int) bits;
            default -> bits;
        };
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        GoKind to = kind();
        if (to == GoKind.UINT64 && from == GoKind.UNSAFE_POINTER) {
            return GoConvert.unsafePointerToUintptr(child.executeLong(frame));
        }
        if (from.isFloatKind()) {
            double d = from == GoKind.FLOAT32 ? (double) child.executeFloat(frame) : child.executeDouble(frame);
            return to == GoKind.UINT64 ? GoConvert.floatToUint64(d) : GoConvert.floatToInt64(d);
        }
        // int/uint -> int64/uint64: the child's extended long is already exact.
        return extendLong(child.executeLong(frame), to);
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        GoKind to = kind();
        if (from.isFloatKind()) {
            double d = from == GoKind.FLOAT32 ? (double) child.executeFloat(frame) : child.executeDouble(frame);
            int w = to == GoKind.UINT32 ? GoConvert.floatToUint32(d) : GoConvert.floatToInt32(d);
            return extendInt(w, to);
        }
        return extendInt(child.executeLong(frame), to);
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        if (from == GoKind.FLOAT64) {
            return (float) child.executeDouble(frame);
        }
        if (from == GoKind.UNTYPED_INT) {
            // Defensive: the factory keeps untyped ints transparent to float
            // kinds, so this only runs on direct construction. The child can
            // be any untyped-constant subtree (unary isn't folded), hence the
            // node protocol instead of a ConstNode cast.
            return Float.parseFloat(child.exactDecimal());
        }
        long v = child.executeLong(frame);
        // from is an int/uint kind (bool/string/float32 sources can't land here).
        return from.isUintKind() ? GoConvert.uint64ToFloat(v) : (float) v;
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        if (from == GoKind.FLOAT32) {
            return (double) child.executeFloat(frame);
        }
        if (from.isFloatKind()) {
            return child.executeDouble(frame);
        }
        if (from == GoKind.UNTYPED_INT) {
            // Defensive: see executeFloat.
            return Double.parseDouble(child.exactDecimal());
        }
        long v = child.executeLong(frame);
        return from.isUintKind() ? GoConvert.uint64ToDouble(v) : (double) v;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return child.executeBoolean(frame);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoKind to = kind();
        if (to == GoKind.STRING && from == GoKind.STRING) {
            return child.executeObject(frame);
        }
        if (to == GoKind.STRING && from == GoKind.UNTYPED_INT) {
            // The child can be any untyped-constant subtree (unary isn't
            // folded by the exporter), hence the node protocol.
            return GoConvert.untypedIntToString(child.exactDecimal());
        }
        if (to == GoKind.STRING) {
            return GoConvert.intToString(child.executeLong(frame));
        }
        throw new UnsupportedOperationException("conversion " + from + " -> " + to + " has no object value");
    }
}
