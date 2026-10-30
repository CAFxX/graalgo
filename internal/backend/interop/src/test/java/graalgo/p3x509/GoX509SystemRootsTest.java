package graalgo.p3x509;

import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.ArrayList;
import java.util.List;

import graalgo.values.GoSlice;

/**
 * Tests for the system-roots trap ({@link GoX509#systemRootCAs}) backing
 * the synthetic crypto/x509 SystemCertPool (review21-tlsverify): nil
 * VerifyOptions.Roots must mean the system roots, not an empty pool.
 * End-to-end verification against a system-trusted chain is covered by
 * the Go differential program
 * test/corpus/wave3/crypto/tls/03_verifycontract.go (SystemCertPool
 * non-empty case); this class pins the trap itself: it must return
 * length-prefixed DERs that all parse as X.509 certificates.
 */
public final class GoX509SystemRootsTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static byte[] unbox(GoSlice gs) {
        byte[] arr = (byte[]) gs.array();
        byte[] out = new byte[gs.len()];
        System.arraycopy(arr, gs.offset(), out, 0, gs.len());
        return out;
    }

    private static List<byte[]> splitLengthPrefixed(byte[] buf) {
        List<byte[]> out = new ArrayList<>();
        int pos = 0;
        while (pos + 4 <= buf.length) {
            int len = ((buf[pos] & 0xFF) << 24) | ((buf[pos + 1] & 0xFF) << 16)
                    | ((buf[pos + 2] & 0xFF) << 8) | (buf[pos + 3] & 0xFF);
            pos += 4;
            if (len < 0 || pos + len > buf.length) {
                break;
            }
            byte[] item = new byte[len];
            System.arraycopy(buf, pos, item, 0, len);
            out.add(item);
        }
        return out;
    }

    public void testSystemRootCAsParse() throws Exception {
        Object out = GoX509.systemRootCAs();
        assertTrue(out instanceof GoSlice, "systemRootCAs must return a GoSlice");
        List<byte[]> ders = splitLengthPrefixed(unbox((GoSlice) out));
        assertTrue(!ders.isEmpty(), "systemRootCAs must yield at least one anchor");
        CertificateFactory cf = CertificateFactory.getInstance("X.509");
        for (byte[] der : ders) {
            X509Certificate cert = (X509Certificate) cf.generateCertificate(
                    new java.io.ByteArrayInputStream(der));
            assertTrue(cert.getSubjectX500Principal() != null,
                    "every system anchor must parse as X.509");
        }
    }
}
