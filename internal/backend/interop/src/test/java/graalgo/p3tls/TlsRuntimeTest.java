package graalgo.p3tls;

import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.ArrayList;
import java.util.List;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Tests for the P3-M9 TLS traps ({@link TlsRuntime}) — review-tls-clientauth
 * (P0-1): server client-auth modes must be honoured, never silently dropped.
 *
 * <p>What this class covers at the Java level: the system-roots trap returns
 * parseable anchors, unknown client-auth values fail loudly before any key
 * parsing, every valid mode passes validation, and a no-certificate client
 * engine can be created. Full end-to-end behaviour of every mode (presence
 * enforcement plus Go-side chain verification, including client certificate
 * presentation) is covered by the Go differential program
 * test/corpus/wave3/crypto/tls/02_clientauth.go, run under both gc and
 * graalgo — the engine-level tests here cannot observe want/need effects
 * without driving a whole handshake, which is exactly what that program does.
 */
public final class TlsRuntimeTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoSlice goBytes(byte[] a) {
        return new GoSlice(a, 0, a.length, a.length);
    }

    private static GoSlice emptyBytes() {
        return goBytes(new byte[0]);
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
            pos += len;
        }
        return out;
    }

    /** Calls newServerEngine with empty chain/key material and the given mode. */
    private static long serverEngineWithMode(long clientAuth) {
        return TlsRuntime.newServerEngine(emptyBytes(), GoString.fromUtf8(""),
                emptyBytes(), Long.valueOf(0), Long.valueOf(0),
                emptyBytes(), Long.valueOf(clientAuth));
    }

    public void testSystemRootCAs() throws Exception {
        Object out = TlsRuntime.systemRootCAs();
        assertTrue(out instanceof GoSlice, "systemRootCAs must return a GoSlice");
        List<byte[]> ders = splitLengthPrefixed(unbox((GoSlice) out));
        assertTrue(!ders.isEmpty(), "systemRootCAs must yield at least one anchor");
        CertificateFactory cf = CertificateFactory.getInstance("X.509");
        X509Certificate cert = (X509Certificate) cf.generateCertificate(
                new java.io.ByteArrayInputStream(ders.get(0)));
        assertTrue(cert.getSubjectX500Principal() != null, "first anchor must parse");
    }

    public void testServerEngineRejectsUnknownClientAuth() {
        for (long bad : new long[]{-1, 5, 99}) {
            try {
                serverEngineWithMode(bad);
                throw new AssertionError("clientAuth " + bad + " must fail loudly");
            } catch (IllegalStateException e) {
                Throwable cause = e.getCause();
                assertTrue(cause != null && cause.getMessage() != null
                        && cause.getMessage().contains("unsupported clientAuth"),
                        "clientAuth " + bad + " must fail on validation, got: " + e);
            }
        }
    }

    public void testServerEngineAcceptsAllModes() {
        // Empty chain/key material still fails (missing key), but it must
        // fail on key parsing, not on client-auth validation: the cause
        // proves validation passed for every mode 0..4.
        for (long mode = 0; mode <= 4; mode++) {
            try {
                serverEngineWithMode(mode);
                throw new AssertionError("empty key material must fail for mode " + mode);
            } catch (IllegalStateException e) {
                Throwable cause = e.getCause();
                assertTrue(cause != null && cause.getMessage() != null
                        && cause.getMessage().contains("unsupported keyAlg"),
                        "mode " + mode + " must reach key parsing, got: " + e);
            }
        }
    }

    public void testClientEngineNoCert() {
        long handle = TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                Long.valueOf(0), Long.valueOf(0), Long.valueOf(0),
                emptyBytes(), emptyBytes(), GoString.fromUtf8(""), emptyBytes());
        assertTrue(handle > 0, "client engine handle must be positive, got " + handle);
    }
}
