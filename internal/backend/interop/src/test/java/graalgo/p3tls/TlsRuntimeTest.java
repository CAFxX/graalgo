package graalgo.p3tls;

import java.io.ByteArrayOutputStream;
import java.security.KeyFactory;
import java.security.interfaces.RSAPrivateCrtKey;
import java.security.spec.PKCS8EncodedKeySpec;
import java.util.Arrays;
import java.util.Base64;

import graalgo.p3x509.GoX509;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for {@link TlsRuntime} (R-03 remediation, 2026-10-31).
 *
 * <p>Previously the TLS intrinsics had no interop unit tests at all:
 * the only coverage was the end-to-end corpus handshake
 * ({@code test/corpus/wave3/crypto/tls/01_handshake.go}). These tests
 * drive two in-memory {@code SSLEngine}s through the public
 * {@code TlsRuntime} API exactly as the synthetic package does
 * (wrap/unwrap/task/pending loop), so a regression in the engine
 * bookkeeping (lost pending bytes, truncated flights, wrong suite
 * mapping) fails here without needing a guest program.
 *
 * <p>Fixtures: one RSA key + self-signed certificate generated with
 * openssl for this test (CN=localhost). The test parses the PEMs with
 * the JDK at runtime and derives the server key parameters from the
 * private key, so the fixture cannot silently drift from the key.
 */
public final class TlsRuntimeTest {

    private static final String CERT_PEM =
            "-----BEGIN CERTIFICATE-----\n"
            + "MIIDPzCCAiegAwIBAgIUHg234//TwusRIM6GXUXyurUEo/8wDQYJKoZIhvcNAQEL\n"
            + "BQAwLzESMBAGA1UEAwwJbG9jYWxob3N0MRkwFwYDVQQKDBBHUkFBTEdPIFRMUyBU\n"
            + "ZXN0MB4XDTI2MTAzMTE2NTg0MVoXDTM2MTAyODE2NTg0MVowLzESMBAGA1UEAwwJ\n"
            + "bG9jYWxob3N0MRkwFwYDVQQKDBBHUkFBTEdPIFRMUyBUZXN0MIIBIjANBgkqhkiG\n"
            + "9w0BAQEFAAOCAQ8AMIIBCgKCAQEAyqBT+DpXgpA8ZS3gizZ/a876468d22umEq9z\n"
            + "oIo8gk5z1nIDAL+5xVLtK5G+qOFKhSqR65aaCNqwJrEL5XMmhNpogowwFyEJOFhQ\n"
            + "6jqhBIGIhf+OlB4znW26Ok/Xawbfgbv78bwcp/nrO+Kp3w9fMuSeR6KlcvYS6yH8\n"
            + "FGnUNK5elyu7n9X4f5Jx/OFzge4cmHQrCNmgO+fUn4WT7kd3mWsfFY7ugluLU9Ek\n"
            + "05vyi/XldiZ7hyJlkrxHfGSceTJmSilkUOG4xY8xR5ZR5lLEL2xN4fI7IZzSnuQ8\n"
            + "KCSePEFKKDAGseqhPyEUVuPfAjZ/coWXWWXqWzM1kkXacmwBZQIDAQABo1MwUTAd\n"
            + "BgNVHQ4EFgQUEcimtBAjSIqY9xZPrwGWI0ZynyswHwYDVR0jBBgwFoAUEcimtBAj\n"
            + "SIqY9xZPrwGWI0ZynyswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOC\n"
            + "AQEAnTIecDanAopumKtGARxR44i1/ILihEroeRqvr9km1156B9KrcPUJWE71pkhV\n"
            + "ggxy5Ad57D7z2Uuk4NytwWMc1XMvROEP+ruIs0PhYQE0+/N3M4G56O7fPfoIWs5H\n"
            + "tWxI6tMhGpOsMjtL8EZQEr+JIoDdvFJL1yXUss7GLAUX8CBdCGO4FwGL0t7BamMK\n"
            + "ayT4IUj7H4k1VjWZHS0Unm2mdHH17aAgR0ng5g0L7DPtc8A5418I4F7dzFOnOt9C\n"
            + "XiUFpgCBqNC07dWXRxPrpfkPlvRHT52HxQn1LFfNKDN1fDm+nvNIDcQBKS3lH+D0\n"
            + "z6dtqhmwUIk50yxUSWwlvU8wmg==\n"
            + "-----END CERTIFICATE-----\n";

    private static final String KEY_PEM =
            "-----BEGIN PRIVATE KEY-----\n"
            + "MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDKoFP4OleCkDxl\n"
            + "LeCLNn9rzvrjrx3ba6YSr3OgijyCTnPWcgMAv7nFUu0rkb6o4UqFKpHrlpoI2rAm\n"
            + "sQvlcyaE2miCjDAXIQk4WFDqOqEEgYiF/46UHjOdbbo6T9drBt+Bu/vxvByn+es7\n"
            + "4qnfD18y5J5HoqVy9hLrIfwUadQ0rl6XK7uf1fh/knH84XOB7hyYdCsI2aA759Sf\n"
            + "hZPuR3eZax8Vju6CW4tT0STTm/KL9eV2JnuHImWSvEd8ZJx5MmZKKWRQ4bjFjzFH\n"
            + "llHmUsQvbE3h8jshnNKe5DwoJJ48QUooMAax6qE/IRRW498CNn9yhZdZZepbMzWS\n"
            + "RdpybAFlAgMBAAECggEAGWPFodENrhqMttDs1gTaKpQg8+65ewkeIjFB99fahSl3\n"
            + "cmOO5MUhbgxaE5+LmHXCdLVE25RfcMSWVgLRTssDBJqphdIzJHLLSPgoC3GVQ49H\n"
            + "oCLJpt2x8gQR1cSzZYHS99qqUzwTCylRxIs+ZejzMNlcy/UhbipPbI8xmw03vfHy\n"
            + "Zq3UJfJpVa8ga1AZAPrT/nWClMrg6vkXREw1E3Qq2964uycHxr1fLW8zTuowhdbf\n"
            + "DvpKynekvmVqoY8XsacTGTSLfWjWG8PIhCezBBKc5+jqy33LAzREK2AkKHWJnoFq\n"
            + "Hl9y+23G+JznQDv8qBEyNKAxJHQUKB/CE0nHQrQngQKBgQDqPNqhmBy7wvmf9KqL\n"
            + "GG1IEAJOxVSTsR5GDbf1Y+JdSe4JEjW1R3X2F5aV4HK56IewxhFpYxzNaSUUZ+hz\n"
            + "hFOZ6BgFQiwoAYYdx6/71kSryKTxQ5URYMOAvrKyUYjh64UuPtEWqq6emGfeAH+d\n"
            + "cyVPuumnuyWrJCp+W9WZKKZhXQKBgQDdc59yckIs5btrlLrzWgvV3VDInWE1Qoam\n"
            + "d8Iav3n6wuhOMT5yiOuiHbqY+/e8EQ6JTGiPz2cBl2FMjMMyiOweDMmGZnwYil38\n"
            + "LvcyKFHmMfnHeFA9zFfnxV/QpYzVAPWy7LBSUsa4wclNXuUJ8WTpNRpSe0xSyS4O\n"
            + "R6aq4+n3qQKBgQC/5R7Pp8hSYkcSreRRk69jjAn38l15qW69wVtlo3C9MGXb9Jkf\n"
            + "a+3NsEFjg785YrBXN8zn45SjC3KfJwkeKdg7jBKxrCZ0l9sB5qfbzwvvF69ZIB6t\n"
            + "X/KKNLA8ueNEAijXXAr7YNUxhHYDP7FMkhqNGPl2ujD/5d8SKbXxhYXY2QKBgBCQ\n"
            + "Y27GCRkeneQwHcOyCoyWn0JCM1Euhq/Q6V3i0JbnGUUv/cHHaAiaPKjQv/j7NaEj\n"
            + "a/bFxqih0A9PwBH5l9+viStaYxHa16jTQn4n8ehL+Kjo4OeebzzQ0JfkqiIMjlQx\n"
            + "zAZ8VozfQdVdsbgKrEedRzjALDdNAPrkdRz3KzrxAoGBANx/JDF18H4ec6PcGFoo\n"
            + "wj2A77DxbwVEbu1iwc/z76UKELNSbR+HaG2FDxHxlB5S3Y2ernXjVdMGUa2GpLnM\n"
            + "z7eJaP+Vv1EK2tT/jvd6vc8AZZe8iXLLDpfV9VY052ZIXc0b7Q9pehWqdzoRGbdK\n"
            + "DifRCJmf0F0am/6g5Z5LMjfe\n"
            + "-----END PRIVATE KEY-----\n";

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static GoSlice slice(byte[] b) {
        return new GoSlice(b.clone(), 0, b.length, b.length);
    }

    private static byte[] bytesOf(Object o) {
        GoSlice gs = (GoSlice) o;
        Object arr = gs.array();
        byte[] out = new byte[gs.len()];
        if (arr instanceof byte[] ba) {
            System.arraycopy(ba, gs.offset(), out, 0, out.length);
        } else if (arr instanceof int[] ia) {
            for (int i = 0; i < out.length; i++) {
                out[i] = (byte) ia[gs.offset() + i];
            }
        } else {
            throw new AssertionError("unexpected backing " + arr.getClass());
        }
        return out;
    }

    private static byte[] pemDecode(String pem) {
        String body = pem.replaceAll("-----[^-]+-----", "").replaceAll("\\s+", "");
        return Base64.getDecoder().decode(body);
    }

    private static byte[] lengthPrefixed(byte[]... items) throws Exception {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        for (byte[] item : items) {
            out.write((item.length >>> 24) & 0xFF);
            out.write((item.length >>> 16) & 0xFF);
            out.write((item.length >>> 8) & 0xFF);
            out.write(item.length & 0xFF);
            out.write(item);
        }
        return out.toByteArray();
    }

    private static byte[] concat(byte[] a, byte[] b) {
        byte[] out = new byte[a.length + b.length];
        System.arraycopy(a, 0, out, 0, a.length);
        System.arraycopy(b, 0, out, 0, b.length);
        return out;
    }

    private static long newServer(byte[] certDer, RSAPrivateCrtKey key) throws Exception {
        byte[] keyParams = lengthPrefixed(
                key.getModulus().toByteArray(),
                key.getPublicExponent().toByteArray(),
                key.getPrivateExponent().toByteArray(),
                key.getPrimeP().toByteArray(),
                key.getPrimeQ().toByteArray());
        return TlsRuntime.newServerEngine(slice(lengthPrefixed(certDer)),
                GoString.fromUtf8("RSA"), slice(keyParams), 0L, 0L, slice(new byte[0]));
    }

    private static long newClient() {
        return TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                0L, 0L, 0L, slice(new byte[0]));
    }

    /** Drives both engines to FINISHED, shuttling flights like the Go driver. */
    private static void handshake(long client, long server) {
        TlsRuntime.beginHandshake(client);
        TlsRuntime.beginHandshake(server);
        byte[] c2s = new byte[0];
        byte[] s2c = new byte[0];
        for (int i = 0; i < 200; i++) {
            long cs = TlsRuntime.engineStatus(client);
            long ss = TlsRuntime.engineStatus(server);
            if (cs == 0 && ss == 0) {
                return;
            }
            if (cs == 3) {
                TlsRuntime.runTasks(client);
            }
            if (ss == 3) {
                TlsRuntime.runTasks(server);
            }
            if (cs == 1) {
                c2s = concat(c2s, bytesOf(TlsRuntime.wrap(client, slice(new byte[0]))));
            }
            if (ss == 1) {
                s2c = concat(s2c, bytesOf(TlsRuntime.wrap(server, slice(new byte[0]))));
            }
            if (c2s.length > 0) {
                TlsRuntime.unwrap(server, slice(c2s));
                c2s = new byte[0];
            }
            if (s2c.length > 0) {
                TlsRuntime.unwrap(client, slice(s2c));
                s2c = new byte[0];
            }
            // Bounded pending drain: one unwrap attempt per loop
            // iteration per side. An unbounded while(hasPending) loop
            // can spin forever when the engine cannot consume the
            // pending bytes yet (it needs the peer's next flight
            // first); the loop's own iterations provide the retries,
            // and the 200-iteration bound fails loud instead of
            // hanging if the handshake genuinely stalls.
            if (TlsRuntime.hasPending(server) == 1) {
                TlsRuntime.unwrap(server, slice(new byte[0]));
            }
            if (TlsRuntime.hasPending(client) == 1) {
                TlsRuntime.unwrap(client, slice(new byte[0]));
            }
        }
        throw new AssertionError("handshake did not finish: client status="
                + TlsRuntime.engineStatus(client) + " server status="
                + TlsRuntime.engineStatus(server));
    }

    public void testHandshakeAndAppDataAndConnectionState() throws Exception {
        byte[] certDer = pemDecode(CERT_PEM);
        RSAPrivateCrtKey key = (RSAPrivateCrtKey) KeyFactory.getInstance("RSA")
                .generatePrivate(new PKCS8EncodedKeySpec(pemDecode(KEY_PEM)));
        long client = newClient();
        long server = newServer(certDer, key);
        try {
            handshake(client, server);
            assertEq(0, TlsRuntime.engineStatus(client), "client must be finished");
            assertEq(0, TlsRuntime.engineStatus(server), "server must be finished");
            assertEq(0, TlsRuntime.hasPending(client), "client must have no pending bytes");
            assertEq(0, TlsRuntime.hasPending(server), "server must have no pending bytes");
            long state = TlsRuntime.connectionState(client);
            assertEq(0x0304, state >>> 32, "negotiated version must be TLS 1.3");
            // Go default suite order (TlsRuntime's documented default):
            // TLS_AES_128_GCM_SHA256 is first and must win on both sides.
            assertEq(0x1301, state & 0xFFFFFFFFL, "negotiated suite must be 0x1301");
            // Application data both directions, through wrap/unwrap.
            byte[] hello = "hello-tls".getBytes(java.nio.charset.StandardCharsets.US_ASCII);
            Object wrapped = TlsRuntime.wrap(client, slice(hello));
            assertTrue(wrapped != null, "client wrap must produce output");
            Object plain = TlsRuntime.unwrap(server, slice(bytesOf(wrapped)));
            assertTrue(plain != null && Arrays.equals(hello, bytesOf(plain)),
                    "server must unwrap the client application data");
            byte[] reply = "reply".getBytes(java.nio.charset.StandardCharsets.US_ASCII);
            Object wrapped2 = TlsRuntime.wrap(server, slice(reply));
            Object plain2 = TlsRuntime.unwrap(client, slice(bytesOf(wrapped2)));
            assertTrue(plain2 != null && Arrays.equals(reply, bytesOf(plain2)),
                    "client must unwrap the server application data");
            // Peer certificates: the server chain, leaf first, length-prefixed.
            byte[] peer = bytesOf(TlsRuntime.peerCertificates(client));
            assertTrue(peer.length >= 4, "peer certificates must be non-empty");
            int len = ((peer[0] & 0xFF) << 24) | ((peer[1] & 0xFF) << 16)
                    | ((peer[2] & 0xFF) << 8) | (peer[3] & 0xFF);
            assertTrue(len == certDer.length
                            && Arrays.equals(certDer, Arrays.copyOfRange(peer, 4, 4 + len)),
                    "first peer certificate must be the server certificate");
            // The RSA peer certificate parses through GoX509 with matching
            // key components (covers the RSA cert accessors GoX509Test
            // cannot reach with its EC-only fixture).
            Object cert = GoX509.parse(slice(Arrays.copyOfRange(peer, 4, 4 + len)));
            assertTrue(cert != null, "peer certificate must parse via GoX509");
            assertTrue(Arrays.equals(key.getModulus().toByteArray(),
                            bytesOf(GoX509.rsaModulus(cert)))
                            || Arrays.equals(stripLeadingZero(key.getModulus().toByteArray()),
                                    bytesOf(GoX509.rsaModulus(cert))),
                    "RSA modulus must match the server key");
            assertEq(65537, (Long) GoX509.rsaExponent(cert), "RSA exponent must be 65537");
            assertTrue(GoX509.ecX(cert) == null, "RSA cert must have no EC X");
            assertEq(-1, (Long) GoX509.ecFieldSize(cert), "RSA cert must have no EC field size");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }

    private static byte[] stripLeadingZero(byte[] b) {
        if (b.length > 1 && b[0] == 0) {
            return Arrays.copyOfRange(b, 1, b.length);
        }
        return b;
    }

    public void testTruncatedFlightDoesNotFinishOrThrow() throws Exception {
        byte[] certDer = pemDecode(CERT_PEM);
        RSAPrivateCrtKey key = (RSAPrivateCrtKey) KeyFactory.getInstance("RSA")
                .generatePrivate(new PKCS8EncodedKeySpec(pemDecode(KEY_PEM)));
        long client = newClient();
        long server = newServer(certDer, key);
        try {
            TlsRuntime.beginHandshake(client);
            TlsRuntime.beginHandshake(server);
            byte[] flight = bytesOf(TlsRuntime.wrap(client, slice(new byte[0])));
            assertTrue(flight.length > 10, "client hello flight must be non-trivial");
            // Feed only the first half: the engine must buffer, not finish
            // and not throw; the remainder arrives with the next unwrap.
            Object out = TlsRuntime.unwrap(server,
                    slice(Arrays.copyOfRange(flight, 0, flight.length / 2)));
            assertTrue(out != null, "truncated unwrap must return (possibly empty) bytes");
            assertTrue(TlsRuntime.engineStatus(server) != 0,
                    "server must not be finished after a truncated flight");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }

    public void testBadHandlesFailLoud() {
        long bad = 999_999_999L;
        assertEq(-1, TlsRuntime.beginHandshake(bad), "beginHandshake on bad handle");
        assertEq(-1, TlsRuntime.engineStatus(bad), "engineStatus on bad handle");
        assertTrue(TlsRuntime.wrap(bad, slice(new byte[0])) == null,
                "wrap on bad handle must be null");
        assertTrue(TlsRuntime.unwrap(bad, slice(new byte[0])) == null,
                "unwrap on bad handle must be null");
        assertEq(-1, TlsRuntime.connectionState(bad), "connectionState on bad handle");
        assertEq(0, TlsRuntime.hasPending(bad), "hasPending on bad handle");
        assertEq(0, bytesOf(TlsRuntime.peerCertificates(bad)).length,
                "peerCertificates on bad handle must be empty");
        assertEq(-1, TlsRuntime.runTasks(bad), "runTasks on bad handle");
    }

    public void testCloseReleasesHandle() {
        long client = newClient();
        TlsRuntime.close(client);
        assertEq(-1, TlsRuntime.engineStatus(client), "closed handle must be unknown");
        assertEq(0, TlsRuntime.hasPending(client), "closed handle must have no pending bytes");
    }

    public void testGarbageServerChainFailsLoud() {
        try {
            TlsRuntime.newServerEngine(slice(new byte[]{1, 2, 3}),
                    GoString.fromUtf8("RSA"), slice(new byte[0]), 0L, 0L,
                    slice(new byte[0]));
            throw new AssertionError("garbage server chain must throw");
        } catch (IllegalStateException expected) {
            assertTrue(expected.getMessage().contains("newServerEngine"),
                    "message must name the failing constructor: " + expected.getMessage());
        }
    }

    public void testInvertedVersionRangeFailsLoudAtHandshake() {
        // min TLS 1.3, max TLS 1.2: no enabled protocol survives. The
        // JDK accepts the empty protocol set at construction, so the
        // failure must surface at handshake start instead: beginHandshake
        // returns -1 (the SSLEngine throws "No appropriate protocol")
        // rather than a dead engine that spins forever.
        long client = TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                0L, 0x0304L, 0x0303L, slice(new byte[0]));
        try {
            assertEq(-1, TlsRuntime.beginHandshake(client),
                    "inverted version range must fail at handshake start");
        } finally {
            TlsRuntime.close(client);
        }
    }

    public void testUnknownCipherSuiteBytesAreIgnored() throws Exception {
        // A client advertising only unknown suite IDs gets no suite
        // restriction (unknown IDs are dropped, yielding null, so the
        // JDK defaults apply on the client). The handshake must still
        // complete, and the server's Go-order preference still selects
        // 0x1301.
        byte[] certDer = pemDecode(CERT_PEM);
        RSAPrivateCrtKey key = (RSAPrivateCrtKey) KeyFactory.getInstance("RSA")
                .generatePrivate(new PKCS8EncodedKeySpec(pemDecode(KEY_PEM)));
        long client = TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                0L, 0L, 0L, slice(new byte[]{0x12, 0x34}));
        long server = newServer(certDer, key);
        try {
            handshake(client, server);
            assertEq(0, TlsRuntime.engineStatus(client), "client must finish");
            assertEq(0x1301, TlsRuntime.connectionState(client) & 0xFFFFFFFFL,
                    "unknown suites must fall back to the Go default order");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }
}
