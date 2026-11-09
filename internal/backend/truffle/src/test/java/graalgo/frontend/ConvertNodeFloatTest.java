package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;

/**
 * B-P1-1 regression (REVIEW-2026-11-09-13): {@link ConvertNode#executeLong}
 * must narrow float-to-narrow-int conversions the way gc/amd64 does.
 *
 * <p>Every expected value below is a stock-gc/amd64 bit pin, measured with
 * {@code go run} (go1.27.1) on 2026-11-09. The float-to-narrow-int
 * mechanism is gc's: float to int8/int16/int32/uint8/uint16 goes through
 * the saturating 32-bit conversion, then truncates (CVTTSD2SI r32), while
 * float to uint32 goes via int64 then takes the low 32 bits (see
 * {@code ValuesTest.testGoConvertFloatToIntNarrow}, which pins the
 * {@code GoConvert} half of the mechanism; this test pins the
 * {@code ConvertNode.executeLong} half, which is the path the
 * index/comparison/slicing consumers read).
 *
 * <p>Red-green: before the fix, every narrow-target case with an
 * out-of-range or wrapping input fails (the float branch returned the raw
 * 64-bit conversion: {@code int32(NaN)} was {@code Long.MIN_VALUE},
 * {@code int32(3e9)} was {@code 3000000000}, {@code uint8(300.0)} was
 * {@code 300}); after the fix all pass. The {@code 2^32+44}/{@code 2^40}
 * cases additionally pin saturation over truncation: the int64-then-truncate
 * spelling is wrong for them ({@code int8(2^32+44)} is 0, not 44), so a
 * fix that merely appends {@code extendLong(floatToInt64(d))} fails here.
 */
public final class ConvertNodeFloatTest {

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0], new FrameDescriptor());
    }

    private static GoExprNode f64(double d) {
        return ConstNode.ofFloat(GoKind.FLOAT64,
                Float.floatToIntBits((float) d), Double.doubleToLongBits(d));
    }

    private static GoExprNode f32(float f) {
        return ConstNode.ofFloat(GoKind.FLOAT32,
                Float.floatToIntBits(f), Double.doubleToLongBits((double) f));
    }

    private static long convLong(GoKind from, GoKind to, GoExprNode child) {
        return new ConvertNode(from, to, child).executeLong(frame());
    }

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    public void testFloat64ToInt32() {
        assertEq(-2147483648L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(Double.NaN)), "int32(NaN)");
        assertEq(-2147483648L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(3e9)), "int32(3e9)");
        assertEq(-2147483648L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(-3e9)), "int32(-3e9)");
        assertEq(-2147483648L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(Double.POSITIVE_INFINITY)), "int32(+Inf)");
        assertEq(-2147483648L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(Double.NEGATIVE_INFINITY)), "int32(-Inf)");
        assertEq(-2147483648L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(1099511627776.0)), "int32(2^40)");
        assertEq(2L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(2.5)), "int32(2.5)");
        assertEq(-2L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(-2.5)), "int32(-2.5)");
        assertEq(123L, convLong(GoKind.FLOAT64, GoKind.INT32, f64(123.999)), "int32(123.999)");
    }

    public void testFloat64ToInt8Int16() {
        assertEq(-56L, convLong(GoKind.FLOAT64, GoKind.INT8, f64(200.0)), "int8(200.0)");
        assertEq(56L, convLong(GoKind.FLOAT64, GoKind.INT8, f64(-200.0)), "int8(-200.0)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.INT8, f64(Double.NaN)), "int8(NaN)");
        // Saturates at 32 bits, then truncates: NOT (byte)(2^32+44) == 44.
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.INT8, f64(4294967340.0)), "int8(2^32+44)");
        assertEq(4464L, convLong(GoKind.FLOAT64, GoKind.INT16, f64(70000.0)), "int16(70000.0)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.INT16, f64(Double.NaN)), "int16(NaN)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.INT16, f64(4294967298.0)), "int16(2^32+2)");
    }

    public void testFloat64ToUint8Uint16() {
        assertEq(44L, convLong(GoKind.FLOAT64, GoKind.UINT8, f64(300.0)), "uint8(300.0)");
        assertEq(255L, convLong(GoKind.FLOAT64, GoKind.UINT8, f64(-1.0)), "uint8(-1.0)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.UINT8, f64(Double.NaN)), "uint8(NaN)");
        // Saturates at 32 bits, then truncates: NOT (2^40+44) & 0xFF == 44.
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.UINT8, f64(1099511627812.0)), "uint8(2^40+44)");
        assertEq(4464L, convLong(GoKind.FLOAT64, GoKind.UINT16, f64(70000.0)), "uint16(70000.0)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.UINT16, f64(Double.NaN)), "uint16(NaN)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.UINT16, f64(1099511627792.0)), "uint16(2^40+4464)");
    }

    public void testFloat64ToUint32() {
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.UINT32, f64(Double.NaN)), "uint32(NaN)");
        assertEq(705032704L, convLong(GoKind.FLOAT64, GoKind.UINT32, f64(5e9)), "uint32(5e9)");
        assertEq(4294967295L, convLong(GoKind.FLOAT64, GoKind.UINT32, f64(-1.0)), "uint32(-1.0)");
        assertEq(3000000000L, convLong(GoKind.FLOAT64, GoKind.UINT32, f64(3e9)), "uint32(3e9)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.UINT32, f64(1.9e19)), "uint32(1.9e19)");
        assertEq(0L, convLong(GoKind.FLOAT64, GoKind.UINT32, f64(Double.POSITIVE_INFINITY)), "uint32(+Inf)");
        assertEq(126456L, convLong(GoKind.FLOAT64, GoKind.UINT32, f64(1099511754232.0)), "uint32(2^40+126456)");
    }

    public void testFloat64ToInt64Uint64Unchanged() {
        // 64-bit targets were already correct; these pin against regressions.
        assertEq(Long.MIN_VALUE, convLong(GoKind.FLOAT64, GoKind.INT64, f64(Double.NaN)), "int64(NaN)");
        assertEq(Long.MIN_VALUE, convLong(GoKind.FLOAT64, GoKind.INT64, f64(1e300)), "int64(1e300)");
        assertEq(123L, convLong(GoKind.FLOAT64, GoKind.INT64, f64(123.999)), "int64(123.999)");
        assertEq(Long.MIN_VALUE, convLong(GoKind.FLOAT64, GoKind.UINT64, f64(Double.NaN)), "uint64(NaN)");
        assertEq(Long.parseUnsignedLong("9300000000000000000"),
                convLong(GoKind.FLOAT64, GoKind.UINT64, f64(9.3e18)), "uint64(9.3e18)");
        assertEq(Long.MIN_VALUE, convLong(GoKind.FLOAT64, GoKind.UINT64, f64(1.9e19)), "uint64(1.9e19)");
    }

    public void testFloat32Source() {
        assertEq(-2147483648L, convLong(GoKind.FLOAT32, GoKind.INT32, f32(Float.NaN)), "int32(float32(NaN))");
        assertEq(-2147483648L, convLong(GoKind.FLOAT32, GoKind.INT32, f32(3e9f)), "int32(float32(3e9))");
        assertEq(44L, convLong(GoKind.FLOAT32, GoKind.UINT8, f32(300.0f)), "uint8(float32(300.0))");
        assertEq(0L, convLong(GoKind.FLOAT32, GoKind.UINT32, f32(Float.NaN)), "uint32(float32(NaN))");
        assertEq(-56L, convLong(GoKind.FLOAT32, GoKind.INT8, f32(200.0f)), "int8(float32(200.0))");
    }
}
