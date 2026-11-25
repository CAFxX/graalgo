package graalgo.go.intrinsics;

import com.oracle.truffle.api.nodes.Node;
import graalgo.go.values.Slice;

/**
 * Package {@code math/big}: the vector kernels upstream implements in amd64 assembly
 * ({@code arith_amd64.s}, declared in {@code arith_decl.go}); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. Each is the word loop of upstream's generic
 * twin ({@code addVV_g}, ... in {@code arith.go}) over the scalar region of the flat value model
 * ({@code []Word} is {@code base.prims[prim .. prim+len)}), with the assembly's contract: the
 * lengths are {@code len(z)}'s (callers pass equal lengths; the assembly checks nothing), and
 * the loops run in the generic code's direction, so the overlapping operands math/big passes
 * ({@code z} aliasing {@code x}) read before they are overwritten exactly as upstream's do.
 *
 * <p>{@code Word} is a defined type, so the generated dispatch passes its arguments boxed
 * ({@code Object}, a {@code Long}).
 *
 * <p>No {@code @TruffleBoundary}: the loops are small, allocation-free and PE-friendly (a
 * multiply-high is {@link Math#unsignedMultiplyHigh}, an intrinsic of the JIT).
 */
final class BigIntrinsics {
    private BigIntrinsics() {}

    private static long[] words(Slice s) {
        return s.base() == null ? null : s.base().prims;
    }

    /** {@code addVV(z, x, y []Word) (c Word)}: z = x + y, the carry out. */
    static long math_big_addVV(Node node, Object z, Object x, Object y) {
        Slice zs = (Slice) z, xs = (Slice) x, ys = (Slice) y;
        int n = zs.len();
        long[] zw = words(zs), xw = words(xs), yw = words(ys);
        int zo = zs.prim(), xo = xs.prim(), yo = ys.prim();
        long c = 0;
        for (int i = 0; i < n; i++) {
            long a = xw[xo + i], b = yw[yo + i];
            long s = a + b + c;
            // bits.Add64's carry: ((a & b) | ((a | b) &^ sum)) >> 63
            c = ((a & b) | ((a | b) & ~s)) >>> 63;
            zw[zo + i] = s;
        }
        return c;
    }

    /** {@code subVV(z, x, y []Word) (c Word)}: z = x - y, the borrow out. */
    static long math_big_subVV(Node node, Object z, Object x, Object y) {
        Slice zs = (Slice) z, xs = (Slice) x, ys = (Slice) y;
        int n = zs.len();
        long[] zw = words(zs), xw = words(xs), yw = words(ys);
        int zo = zs.prim(), xo = xs.prim(), yo = ys.prim();
        long c = 0;
        for (int i = 0; i < n; i++) {
            long a = xw[xo + i], b = yw[yo + i];
            long d = a - b - c;
            // bits.Sub64's borrow: ((^a & b) | (^(a ^ b) & diff)) >> 63
            c = ((~a & b) | (~(a ^ b) & d)) >>> 63;
            zw[zo + i] = d;
        }
        return c;
    }

    /** {@code lshVU(z, x []Word, s uint) (c Word)}: z = x << s, the bits shifted out; 0 < s < 64 in use. */
    static long math_big_lshVU(Node node, Object z, Object x, long s) {
        Slice zs = (Slice) z, xs = (Slice) x;
        int n = zs.len();
        long[] zw = words(zs), xw = words(xs);
        int zo = zs.prim(), xo = xs.prim();
        if (s == 0) {
            if (n > 0) {
                System.arraycopy(xw, xo, zw, zo, n);
            }
            return 0;
        }
        if (n == 0) {
            return 0;
        }
        int sh = (int) (s & 63);
        int sb = (64 - sh) & 63;
        long c = xw[xo + n - 1] >>> sb;
        for (int i = n - 1; i > 0; i--) {
            zw[zo + i] = (xw[xo + i] << sh) | (xw[xo + i - 1] >>> sb);
        }
        zw[zo] = xw[xo] << sh;
        return c;
    }

    /** {@code rshVU(z, x []Word, s uint) (c Word)}: z = x >> s, the bits shifted out (at the top). */
    static long math_big_rshVU(Node node, Object z, Object x, long s) {
        Slice zs = (Slice) z, xs = (Slice) x;
        int n = zs.len();
        long[] zw = words(zs), xw = words(xs);
        int zo = zs.prim(), xo = xs.prim();
        if (s == 0) {
            if (n > 0) {
                System.arraycopy(xw, xo, zw, zo, n);
            }
            return 0;
        }
        if (n == 0) {
            return 0;
        }
        int sh = (int) (s & 63);
        int sb = (64 - sh) & 63;
        long c = xw[xo] << sb;
        for (int i = 1; i < n; i++) {
            zw[zo + i - 1] = (xw[xo + i - 1] >>> sh) | (xw[xo + i] << sb);
        }
        zw[zo + n - 1] = xw[xo + n - 1] >>> sh;
        return c;
    }

    /** {@code mulAddVWW(z, x []Word, m, a Word) (c Word)}: z = x*m + a, the high word out. */
    static long math_big_mulAddVWW(Node node, Object z, Object x, Object mw, Object aw) {
        long m = (Long) mw, a = (Long) aw;
        Slice zs = (Slice) z, xs = (Slice) x;
        int n = zs.len();
        long[] zw = words(zs), xw = words(xs);
        int zo = zs.prim(), xo = xs.prim();
        long c = a;
        for (int i = 0; i < n; i++) {
            long xi = xw[xo + i];
            long hi = Math.unsignedMultiplyHigh(xi, m);
            long lo = xi * m;
            long sum = lo + c;
            hi += Long.compareUnsigned(sum, lo) < 0 ? 1 : 0;
            zw[zo + i] = sum;
            c = hi;
        }
        return c;
    }

    /** {@code addMulVVWW(z, x, y []Word, m, a Word) (c Word)}: z = x + y*m + a, the high word out. */
    static long math_big_addMulVVWW(Node node, Object z, Object x, Object y, Object mw, Object aw) {
        long m = (Long) mw, a = (Long) aw;
        Slice zs = (Slice) z, xs = (Slice) x, ys = (Slice) y;
        int n = zs.len();
        long[] zw = words(zs), xw = words(xs), yw = words(ys);
        int zo = zs.prim(), xo = xs.prim(), yo = ys.prim();
        long c = a;
        for (int i = 0; i < n; i++) {
            long yi = yw[yo + i];
            // z1, z0 := mulAddWWW_g(y[i], m, x[i])
            long hi = Math.unsignedMultiplyHigh(yi, m);
            long lo = yi * m;
            long z0 = lo + xw[xo + i];
            hi += Long.compareUnsigned(z0, lo) < 0 ? 1 : 0;
            // lo, cc := bits.Add(z0, c, 0)
            long s = z0 + c;
            long cc = Long.compareUnsigned(s, z0) < 0 ? 1 : 0;
            zw[zo + i] = s;
            c = cc + hi;
        }
        return c;
    }
}
