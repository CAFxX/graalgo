package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;

/**
 * PKG-026 regression: {@link BinaryOpNode} must accept the full complex
 * arithmetic matrix (ADD/SUB/MUL/DIV on complex64 and complex128) — the
 * frontend used to crash building {@code math/cmplx} with
 * {@code IllegalArgumentException: illegal binary op/kind combination:
 * MUL result=COMPLEX128 operand=COMPLEX128}.
 *
 * <p>Every expected value below is a stock-gc/amd64 bit pin, measured with
 * {@code go run} on 2026-10-15. NaN components assert {@code isNaN} only:
 * NaN payloads are unspecified by Go and differ between gc's hardware
 * division and the JVM.
 *
 * <p>Red-green: before the fix, every construction test fails with the
 * exact reported {@code IllegalArgumentException}; after the fix all pass.
 * The rejection tests are controls that pass before and after.
 */
public final class ComplexBinaryOpTest {

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0], new FrameDescriptor());
    }

    private static GoExprNode c128(double re, double im) {
        return ConstNode.ofObject(GoKind.COMPLEX128, new GoComplex64(re, im));
    }

    private static GoExprNode c64(float re, float im) {
        return ConstNode.ofObject(GoKind.COMPLEX64, new GoComplex32(re, im));
    }

    private static GoComplex64 run128(BinaryOpNode.Op op,
            double ar, double ai, double br, double bi) {
        BinaryOpNode n = new BinaryOpNode(op, GoKind.COMPLEX128,
                c128(ar, ai), c128(br, bi));
        return (GoComplex64) n.executeObject(frame());
    }

    private static GoComplex32 run64(BinaryOpNode.Op op,
            float ar, float ai, float br, float bi) {
        BinaryOpNode n = new BinaryOpNode(op, GoKind.COMPLEX64,
                c64(ar, ai), c64(br, bi));
        return (GoComplex32) n.executeObject(frame());
    }

    private static void assertBits64(long want, double got, String what) {
        long g = Double.doubleToLongBits(got);
        if (want != g) {
            throw new AssertionError(what + ": want bits " + Long.toHexString(want)
                    + " got " + Long.toHexString(g));
        }
    }

    private static void assertBits32(int want, float got, String what) {
        int g = Float.floatToIntBits(got);
        if (want != g) {
            throw new AssertionError(what + ": want bits " + Integer.toHexString(want)
                    + " got " + Integer.toHexString(g));
        }
    }

    private static void assertNaN(double got, String what) {
        if (!Double.isNaN(got)) {
            throw new AssertionError(what + ": want NaN got " + got);
        }
    }

    private static void expectIllegal(Runnable r, String what) {
        try {
            r.run();
        } catch (IllegalArgumentException e) {
            if (!e.getMessage().contains("illegal binary op/kind combination")) {
                throw new AssertionError(what + ": wrong message: " + e.getMessage());
            }
            return;
        }
        throw new AssertionError(what + ": expected IllegalArgumentException");
    }

    // --- complex128: the full arithmetic matrix ---

    public void testAdd128() {
        GoComplex64 r = run128(BinaryOpNode.Op.ADD, 1, 2, 3, 4);
        assertBits64(0x4010000000000000L, r.re(), "add128.re");
        assertBits64(0x4018000000000000L, r.im(), "add128.im");
    }

    public void testSub128() {
        GoComplex64 r = run128(BinaryOpNode.Op.SUB, 1, 2, 3, 4);
        assertBits64(0xc000000000000000L, r.re(), "sub128.re");
        assertBits64(0xc000000000000000L, r.im(), "sub128.im");
    }

    public void testMul128() {
        // The exact reported crash: MUL result=COMPLEX128 operand=COMPLEX128.
        GoComplex64 r = run128(BinaryOpNode.Op.MUL, 1, 2, 3, 4);
        assertBits64(0xc014000000000000L, r.re(), "mul128.re");
        assertBits64(0x4024000000000000L, r.im(), "mul128.im");
    }

    public void testDiv128() {
        GoComplex64 r = run128(BinaryOpNode.Op.DIV, 1, 2, 3, 4);
        assertBits64(0x3fdc28f5c28f5c29L, r.re(), "div128.re");
        assertBits64(0x3fb47ae147ae147bL, r.im(), "div128.im");
    }

    public void testDiv128ByZero() {
        GoComplex64 r = run128(BinaryOpNode.Op.DIV, 1, 2, 0, 0);
        assertBits64(0x7ff0000000000000L, r.re(), "div128by0.re");
        assertBits64(0x7ff0000000000000L, r.im(), "div128by0.im");
    }

    public void testDiv128ZeroByZero() {
        GoComplex64 r = run128(BinaryOpNode.Op.DIV, 0, 0, 0, 0);
        assertNaN(r.re(), "div128zero.re");
        assertNaN(r.im(), "div128zero.im");
    }

    public void testMul128Overflow() {
        GoComplex64 r = run128(BinaryOpNode.Op.MUL, 1e300, 1e300, 1e300, 1e300);
        assertNaN(r.re(), "mul128ovf.re");
        assertBits64(0x7ff0000000000000L, r.im(), "mul128ovf.im");
    }

    public void testDiv128InfCorrection() {
        // Exercises the C99 NaN-correction branch of runtime.complex128div:
        // (inf+1i)/(2+3i) must correct to (+inf,-inf), not (nan,nan).
        GoComplex64 r = run128(BinaryOpNode.Op.DIV,
                Double.POSITIVE_INFINITY, 1, 2, 3);
        assertBits64(0x7ff0000000000000L, r.re(), "div128inf.re");
        assertBits64(0xfff0000000000000L, r.im(), "div128inf.im");
    }

    public void testAdd128NaNPropagates() {
        GoComplex64 r = run128(BinaryOpNode.Op.ADD, Double.NaN, 0, 1, 1);
        assertNaN(r.re(), "add128nan.re");
        assertBits64(0x3ff0000000000000L, r.im(), "add128nan.im");
    }

    // --- complex64: the full arithmetic matrix ---

    public void testAdd64() {
        GoComplex32 r = run64(BinaryOpNode.Op.ADD, 1f, 2f, 3f, 4f);
        assertBits32(0x40800000, r.re(), "add64.re");
        assertBits32(0x40c00000, r.im(), "add64.im");
    }

    public void testSub64() {
        GoComplex32 r = run64(BinaryOpNode.Op.SUB, 1f, 2f, 3f, 4f);
        assertBits32(0xc0000000, r.re(), "sub64.re");
        assertBits32(0xc0000000, r.im(), "sub64.im");
    }

    public void testMul64() {
        GoComplex32 r = run64(BinaryOpNode.Op.MUL, 1f, 2f, 3f, 4f);
        assertBits32(0xc0a00000, r.re(), "mul64.re");
        assertBits32(0x41200000, r.im(), "mul64.im");
    }

    public void testMul64Rounding() {
        // gc widens complex64 components to float64 for the multiply and
        // rounds once; pins the (1.1+2.2i)*(3.3+4.4i) rounding behavior.
        GoComplex32 r = run64(BinaryOpNode.Op.MUL, 1.1f, 2.2f, 3.3f, 4.4f);
        assertBits32(0xc0c1999a, r.re(), "mul64round.re");
        assertBits32(0x4141999a, r.im(), "mul64round.im");
    }

    public void testMul64OverflowWidens() {
        // The discriminating pin: naive float32 arithmetic gives (nan,+inf)
        // here; gc gives (0,+inf) because the multiply happens in float64.
        GoComplex32 r = run64(BinaryOpNode.Op.MUL, 1e30f, 1e30f, 1e30f, 1e30f);
        assertBits32(0x00000000, r.re(), "mul64ovf.re");
        assertBits32(0x7f800000, r.im(), "mul64ovf.im");
    }

    public void testDiv64() {
        GoComplex32 r = run64(BinaryOpNode.Op.DIV, 1f, 2f, 3f, 4f);
        assertBits32(0x3ee147ae, r.re(), "div64.re");
        assertBits32(0x3da3d70a, r.im(), "div64.im");
    }

    public void testDiv64ByZero() {
        GoComplex32 r = run64(BinaryOpNode.Op.DIV, 1f, 2f, 0f, 0f);
        assertBits32(0x7f800000, r.re(), "div64by0.re");
        assertBits32(0x7f800000, r.im(), "div64by0.im");
    }

    public void testDiv64InfNarrowed() {
        // (inf+1i)/(2+3i): Smith in float64, then narrowed to complex64.
        GoComplex32 r = run64(BinaryOpNode.Op.DIV, Float.POSITIVE_INFINITY, 1f, 2f, 3f);
        assertBits32(0x7f800000, r.re(), "div64inf.re");
        assertBits32(0xff800000, r.im(), "div64inf.im");
    }

    // --- rejections: controls that pass before and after the fix ---

    public void testRejectMod128() {
        expectIllegal(() -> new BinaryOpNode(BinaryOpNode.Op.MOD, GoKind.COMPLEX128,
                c128(1, 2), c128(3, 4)), "mod128");
    }

    public void testRejectShl64() {
        expectIllegal(() -> new BinaryOpNode(BinaryOpNode.Op.SHL, GoKind.COMPLEX64,
                c64(1f, 2f), c64(3f, 4f)), "shl64");
    }

    public void testRejectAndNot128() {
        expectIllegal(() -> new BinaryOpNode(BinaryOpNode.Op.AND_NOT, GoKind.COMPLEX128,
                c128(1, 2), c128(3, 4)), "andnot128");
    }

    public void testRejectMismatchedComplexKinds() {
        // Arithmetic requires result kind == operand kind.
        expectIllegal(() -> new BinaryOpNode(BinaryOpNode.Op.MUL, GoKind.COMPLEX128,
                GoKind.COMPLEX64, c64(1f, 2f), c64(3f, 4f)), "mul128/64");
    }
}
