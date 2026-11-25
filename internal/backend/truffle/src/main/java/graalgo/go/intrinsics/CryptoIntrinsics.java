package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.Unsafe;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;

/**
 * Crypto's runtime floor: {@code crypto/internal/boring/sig}'s assembly markers,
 * {@code crypto/internal/constanttime}'s compiler intrinsics, and package
 * {@code crypto/internal/fips140/subtle}'s {@code xorBytes}, registered in
 * {@code internal/toolexec/intrinsics/registry.txt} as a {@code TEMPORARY (BUG-0058)}
 * {@code replace}. On amd64 upstream implements it in assembly ({@code xor_amd64.s}); under
 * {@code purego} (the launcher's default, ADR-0010 DV3) its Go body ({@code xor_generic.go})
 * views the {@code []byte} operands as {@code []uintptr} through {@code unsafe.Slice}, which the
 * flat value model cannot give (byte leaves live in the byte region, words in the scalar one;
 * the checkptr fatal error before the stop-gap) — BUG-0037's class, the other direction.
 */
final class CryptoIntrinsics {
    private CryptoIntrinsics() {}

    /** {@code xorBytes(dst, x, y *byte, n int)}: dst[i] = x[i] ^ y[i] for i < n. */
    @TruffleBoundary
    static void crypto_internal_fips140_subtle_xorBytes(Node node, Object dst, Object x, Object y, long n) {
        int len = (int) n;
        if (len <= 0) {
            return;
        }
        byte[] xb = bytes(node, x, len);
        byte[] yb = bytes(node, y, len);
        if (direct(dst) != null) {
            byte[] d = direct(dst);
            int o = offset(dst);
            for (int i = 0; i < len; i++) {
                d[o + i] = (byte) (xb[i] ^ yb[i]);
            }
            return;
        }
        byte[] out = new byte[len];
        for (int i = 0; i < len; i++) {
            out[i] = (byte) (xb[i] ^ yb[i]);
        }
        Unsafe.writeBytes(node, Unsafe.referent(dst), Unsafe.offset(dst), out, len);
    }

    /** The byte region a pointer to a byte leaf indexes, or null for any other pointer. */
    private static byte[] direct(Object p) {
        if (p instanceof Flat f && f.bytes.length > 0 && f.prims.length == 0 && f.refs.length == 0) {
            return f.bytes;
        } else if (p instanceof Ptr q && q.obj().prims.length == 0 && q.obj().refs.length == 0) {
            return q.obj().bytes;
        }
        return null;
    }

    private static int offset(Object p) {
        return p instanceof Ptr q ? q.byt() : 0;
    }

    /** A copy of the n bytes at p (x and y may overlap dst). */
    private static byte[] bytes(Node node, Object p, int n) {
        byte[] out = new byte[n];
        byte[] d = direct(p);
        if (d != null) {
            System.arraycopy(d, offset(p), out, 0, n);
        } else {
            Unsafe.readBytes(node, Unsafe.referent(p), Unsafe.offset(p), out, n);
        }
        return out;
    }

    /** {@code crypto/internal/boring/sig.BoringCrypto}: a marker; nothing to do. */
    static void crypto_internal_boring_sig_BoringCrypto(Node node) {
    }

    /** {@code crypto/internal/boring/sig.FIPSOnly}: a marker; nothing to do. */
    static void crypto_internal_boring_sig_FIPSOnly(Node node) {
    }

    /** {@code crypto/internal/boring/sig.StandardCrypto}: a marker; nothing to do. */
    static void crypto_internal_boring_sig_StandardCrypto(Node node) {
    }

    /** {@code crypto/internal/constanttime.boolToUint8(b bool) uint8} (gc: {@code CvtBoolToUint8}). */
    static long crypto_internal_constanttime_boolToUint8(Node node, boolean b) {
        return b ? 1 : 0;
    }

    /** {@code crypto/internal/constanttime.Select(v, x, y int) int}: x if v != 0, else y (gc: a conditional move). */
    static long crypto_internal_constanttime_Select(Node node, long v, long x, long y) {
        return v != 0 ? x : y;
    }
}
