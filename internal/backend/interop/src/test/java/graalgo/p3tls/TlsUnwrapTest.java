package graalgo.p3tls;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.math.BigInteger;
import java.nio.ByteBuffer;
import java.security.KeyFactory;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.security.spec.RSAPrivateCrtKeySpec;
import java.util.Base64;
import javax.net.ssl.KeyManager;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLEngineResult;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509ExtendedKeyManager;
import javax.net.ssl.X509TrustManager;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Tests for the {@link TlsRuntime#unwrap} close/error contract
 * (REVIEW-2026-11-03-13 P1-2): unwrap must not conflate peer-close and
 * protocol error into one null, and must not discard app bytes produced
 * with the close.
 *
 * <p>The tests drive a real handshake between a {@link TlsRuntime}
 * client engine and a raw JDK server engine (the server side needs a
 * certificate; the fixtures are copied verbatim from
 * test/corpus/wave3/crypto/tls/01_handshake.go, same precedent as
 * test/regression/tls-wrap-hang). The client side is the code under
 * test: {@code TlsRuntime.unwrap} + {@code TlsRuntime.unwrapState}.
 *
 * <p>{@code testAppDataWithCloseNotify} is the failing-capable test for
 * the data-loss half: it feeds app data and the peer's close_notify in
 * ONE unwrap call. Pre-fix, unwrap returned null there and the app
 * bytes were lost. {@code testCorruptRecord} is the failing-capable
 * test for the conflation half: pre-fix, unwrap returned null with no
 * recorded state, indistinguishable from a clean close.
 */
public final class TlsUnwrapTest {

    // ---- small helpers ----

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    private static void assertBytes(byte[] want, byte[] got, String msg) {
        if (!java.util.Arrays.equals(want, got)) {
            throw new AssertionError(msg + ": want " + new String(want)
                    + " got " + (got == null ? "null" : new String(got)));
        }
    }

    private static GoSlice slice(byte[] b) {
        return new GoSlice(b, 0, b.length, b.length);
    }

    private static byte[] goBytes(Object o) {
        assertTrue(o instanceof GoSlice, "unwrap/wrap must return a GoSlice, got " + o);
        GoSlice gs = (GoSlice) o;
        byte[] arr = (byte[]) gs.array();
        byte[] out = new byte[gs.len()];
        System.arraycopy(arr, gs.offset(), out, 0, gs.len());
        return out;
    }

    // ---- fixtures (PEM bodies filled in below) ----

        private static final String SRV_PEM =
        "-----BEGIN CERTIFICATE-----\n"
        + "MIIDGjCCAgKgAwIBAgIBAjANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFh\n"
        + "bGdvLXRlc3QtY2EwHhcNMjAwMTAxMDAwMDAwWhcNMzAwMTAxMDAwMDAwWjAUMRIw\n"
        + "EAYDVQQDEwlsb2NhbGhvc3QwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIB\n"
        + "AQCt1dUUe3CCvA2lW3k9CTei512AP2p+IUEDuoaxyi+cEwEFQKtqE8/VS59lD3KB\n"
        + "ZBc1Vls20uf3LmsMX/fI062SEZm+D1KQrGj4azDkUFbtGGGRV+ujmBdE6SBomWnb\n"
        + "AJrF9CeAXIJT1QC/JUqydFXiZt/TUN6+h8pG8+Fb2tbDLGRV86ZlemW66Ro1W7jm\n"
        + "uFQOvlbVdoas2DLWHLZU5RM7U0qtjshCEOt5kxXpayxRMm+ax3MGlogUkX4gwczM\n"
        + "jB6msEQwuFs7rvog+shHysioF1Q6U/Q6fejSSm6IKTqrCf2RDUHDCXHyCMQ4DLBe\n"
        + "lqmkI9v/SuW4124vRIqsAD3pAgMBAAGjcTBvMA4GA1UdDwEB/wQEAwIHgDATBgNV\n"
        + "HSUEDDAKBggrBgEFBQcDATAfBgNVHSMEGDAWgBQjW9fRc89jeQWJUkgdXjdiFTrF\n"
        + "NDAnBgNVHREEIDAegglsb2NhbGhvc3SCC2V4YW1wbGUuY29thwR/AAABMA0GCSqG\n"
        + "SIb3DQEBCwUAA4IBAQCv149c0cHrbjaiJMnTrTKzloTMZVaqbcBIdmZDKbz0WFmn\n"
        + "EXW3eQy4KCC5II3rdOf3jDAdE8LWP6yfshDrIoMy/Ze1r896u8yE/LlzDxyxjr+Q\n"
        + "LYv4snQRJk72bAYeFAwpwLVulLYlZh29V+DNWL6a/+4VcStQLJ8adRA4RqtgMesp\n"
        + "3mE+/POgpwCmz5kP1+rA1noaWh2PslS4k6Igvn60WsZ3vVCX37J9js6ML3Zxhadn\n"
        + "nyvcveOSGnYVN3vN/ZqzUUFGRGZDkC0lEcSb6+qXZIQOVGfTT4j5PmQEfImfmpAX\n"
        + "mS98pvkmVmYJJFgkLNz9bVYiKxbYq3leFurZFGUO\n"
        + "-----END CERTIFICATE-----\n";

    private static final String SRV_KEY_PEM =
        "-----BEGIN RSA PRIVATE KEY-----\n"
        + "MIIEogIBAAKCAQEArdXVFHtwgrwNpVt5PQk3ouddgD9qfiFBA7qGscovnBMBBUCr\n"
        + "ahPP1UufZQ9ygWQXNVZbNtLn9y5rDF/3yNOtkhGZvg9SkKxo+Gsw5FBW7RhhkVfr\n"
        + "o5gXROkgaJlp2wCaxfQngFyCU9UAvyVKsnRV4mbf01DevofKRvPhW9rWwyxkVfOm\n"
        + "ZXpluukaNVu45rhUDr5W1XaGrNgy1hy2VOUTO1NKrY7IQhDreZMV6WssUTJvmsdz\n"
        + "BpaIFJF+IMHMzIweprBEMLhbO676IPrIR8rIqBdUOlP0On3o0kpuiCk6qwn9kQ1B\n"
        + "wwlx8gjEOAywXpappCPb/0rluNduL0SKrAA96QIDAQABAoIBAChrdIi3LRGQ9WFS\n"
        + "FC4vgvrDblfAJxxd0hayW9hiKVDSkGWuxEy9f5FabJ0ciuTwJ39U7Y+hWaN9bRGr\n"
        + "DNvq8ExrAEMToACYvi2pCuzDi3rEtQcNGPLojdyNkmYP6e+sw9AsdbTgjcM9X1eS\n"
        + "rbwx1bh3cCrb/nbx7wQxd+CaUV5/BUyU36l9LShtpigsonjL0P0Ew+fm79bOZpOe\n"
        + "ap1zBWkhnhnOSA4eRynqpXak3KO1HIFgh1T45e9q3yXwUV/PLKLNkEXCRAWaOiHh\n"
        + "xBllZU+ewP/9eSiRvz9e+TVfbk/phbBRm37MwBDdZttSBPrVa99cHxZfj+4Rd1VK\n"
        + "sbPyYeMCgYEA2b9y7gn/q5HDKItIZuNdLx1aj49JHyFWtf0PgupFUhksDPg8nGp9\n"
        + "wSWFGUd5TaRLqod9ducEbOQRdJzUDOo9fkzd8lklqdMtF/0qECJR8F16FLye4ajf\n"
        + "Xc+4vv+Me3Hv+l5+r1af6AcBO8vHDkM7CS1aF3rLPsVPU5IcQEumklcCgYEAzF+M\n"
        + "bwm6Inen4POnEepjFfGWS8LQ3lSb9zt2pdC4liYxbznr/YTddOX2Qm4LpwJH7S0T\n"
        + "ZCVpwVcTPf3voDb3uSOUspoi670OKgGuKbePoUfzT2NBwgXMV3eOtfhvqYmXT22a\n"
        + "NXKqfPJnrEtuUzoem3yTYUY4XdZi3Gro0miMCb8CgYBvVZFJHrmoGtZjU5wIZ3ZM\n"
        + "0cBtKMEldqFkWejlci1RheddFku/Wj/EUw/T1hSy2y+JGM/QzNwZZR97tXhCxj38\n"
        + "5waQlH5Kb9xdqKcqKrIkqVbzZWIDVZon2D7JAdbtg1tFc61or20BcRtzXIUByORV\n"
        + "k+e9m55yEOa3fzyMr2GS7wKBgBhce1zxB+L76GMEKDIWgmEnPwcg3cziwQSjbe0u\n"
        + "CBK90r6zZNH/eHZqwwQAIhtyRiibauhRHV4ShxSopFuwsa678wmnHeqDyqBvOtd2\n"
        + "LgTHktUvu7LpBgaXFcfTKVxILmUlZ3Bfjrr+GiWLiEcbcnKOUvMRb76fw4EKrV4H\n"
        + "YEn5AoGAI5nb8K9GaCSoPKVojgUezQHpmJ55x1S+XGAx2BjZphZj27OvqNE7NB9R\n"
        + "O2twfPCYDMvTQiE8cmquId60jxYnCWblSoW2qEWzVizIKuCbBjuGswdgqAWp8IX0\n"
        + "C5baQ4MR6V6bahfKjdlUCWpAhRpl8DgJ/k/hVMPWltreAOlSN9Y=\n"
        + "-----END RSA PRIVATE KEY-----\n";

    private static byte[] pemDer(String pem) throws Exception {
        StringBuilder sb = new StringBuilder();
        for (String line : pem.split("\n")) {
            if (!line.startsWith("-----")) {
                sb.append(line.trim());
            }
        }
        return Base64.getDecoder().decode(sb.toString());
    }

    /** Minimal PKCS#1 RSAPrivateKey parser: SEQUENCE of INTEGERs. */
    private static BigInteger[] parsePkcs1(byte[] der) throws Exception {
        // der = SEQUENCE { version, n, e, d, p, q, dp, dq, qinv }.
        int[] pos = {0};
        int tag = der[pos[0]++] & 0xFF;
        assertTrue(tag == 0x30, "PKCS#1 must start with SEQUENCE");
        readLen(der, pos);
        BigInteger[] out = new BigInteger[9];
        for (int i = 0; i < 9; i++) {
            int t = der[pos[0]++] & 0xFF;
            assertTrue(t == 0x02, "PKCS#1 field " + i + " must be INTEGER");
            int len = readLen(der, pos);
            byte[] v = new byte[len];
            System.arraycopy(der, pos[0], v, 0, len);
            pos[0] += len;
            out[i] = new BigInteger(1, v);
        }
        return out;
    }

    private static int readLen(byte[] der, int[] pos) {
        int b = der[pos[0]++] & 0xFF;
        if ((b & 0x80) == 0) {
            return b;
        }
        int n = b & 0x7F;
        int len = 0;
        for (int i = 0; i < n; i++) {
            len = (len << 8) | (der[pos[0]++] & 0xFF);
        }
        return len;
    }

    private static SSLEngine newServerEngine() throws Exception {
        byte[] certDer = pemDer(SRV_PEM);
        X509Certificate cert = (X509Certificate) CertificateFactory.getInstance("X.509")
                .generateCertificate(new ByteArrayInputStream(certDer));
        BigInteger[] k = parsePkcs1(pemDer(SRV_KEY_PEM));
        BigInteger dp = k[3].mod(k[4].subtract(BigInteger.ONE));
        BigInteger dq = k[3].mod(k[5].subtract(BigInteger.ONE));
        BigInteger qinv = k[5].modInverse(k[4]);
        RSAPrivateCrtKeySpec spec = new RSAPrivateCrtKeySpec(
                k[1], k[2], k[3], k[4], k[5], dp, dq, qinv);
        java.security.PrivateKey key =
                KeyFactory.getInstance("RSA").generatePrivate(spec);
        X509Certificate[] chain = {cert};
        X509ExtendedKeyManager km = new X509ExtendedKeyManager() {
            @Override
            public String chooseEngineServerAlias(String keyType,
                    java.security.Principal[] issuers, SSLEngine engine) {
                return "graalgo";
            }
            @Override
            public X509Certificate[] getCertificateChain(String alias) {
                return chain.clone();
            }
            @Override
            public java.security.PrivateKey getPrivateKey(String alias) {
                return key;
            }
            @Override
            public String[] getServerAliases(String keyType,
                    java.security.Principal[] issuers) {
                return new String[]{"graalgo"};
            }
            @Override
            public String chooseServerAlias(String keyType,
                    java.security.Principal[] issuers, java.net.Socket socket) {
                return "graalgo";
            }
            @Override
            public String chooseClientAlias(String[] keyType,
                    java.security.Principal[] issuers, java.net.Socket socket) {
                return null;
            }
            @Override
            public String[] getClientAliases(String keyType,
                    java.security.Principal[] issuers) {
                return new String[0];
            }
        };
        TrustManager permissive = new X509TrustManager() {
            @Override
            public void checkClientTrusted(X509Certificate[] c, String a) {}
            @Override
            public void checkServerTrusted(X509Certificate[] c, String a) {}
            @Override
            public X509Certificate[] getAcceptedIssuers() {
                return new X509Certificate[0];
            }
        };
        SSLContext ctx = SSLContext.getInstance("TLS");
        ctx.init(new KeyManager[]{km}, new TrustManager[]{permissive},
                new java.security.SecureRandom());
        SSLEngine engine = ctx.createSSLEngine();
        engine.setUseClientMode(false);
        engine.setNeedClientAuth(false);
        return engine;
    }

    private static long newClientHandle() {
        return TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                Long.valueOf(0), Long.valueOf(0), Long.valueOf(0),
                new GoSlice(new byte[0], 0, 0, 0));
    }

    /** One handshaked pair: TlsRuntime client engine + raw JDK server engine. */
    private static final class Pair {
        final long client;
        final SSLEngine server;

        Pair(long client, SSLEngine server) {
            this.client = client;
            this.server = server;
        }

        void close() {
            TlsRuntime.close(Long.valueOf(client));
            try {
                server.closeInbound();
            } catch (Exception ignored) {
            }
            try {
                server.closeOutbound();
            } catch (Exception ignored) {
            }
        }
    }

    private static Pair handshake() throws Exception {
        long client = newClientHandle();
        SSLEngine server = newServerEngine();
        try {
            assertEq(0, TlsRuntime.beginHandshake(Long.valueOf(client)),
                    "client beginHandshake");
            server.beginHandshake();
            ByteArrayOutputStream c2s = new ByteArrayOutputStream();
            ByteArrayOutputStream s2c = new ByteArrayOutputStream();
            for (int i = 0; i < 100; i++) {
                long cst = TlsRuntime.engineStatus(Long.valueOf(client));
                SSLEngineResult.HandshakeStatus sst = server.getHandshakeStatus();
                // Do NOT return while either flight still holds bytes:
                // under TLS 1.3 the server sends NewSessionTicket
                // post-handshake records after Finished, and the client
                // must unwrap them before any app-data record — the
                // record sequence numbers are shared, so dropping the
                // tickets makes the next app record fail MAC
                // verification (observed: app unwrap reported ERROR
                // with empty bytes). The client-unwrap branch below
                // therefore also runs when cst == 0 with bytes queued.
                if (cst == 0 && (sst == SSLEngineResult.HandshakeStatus.FINISHED
                        || sst == SSLEngineResult.HandshakeStatus.NOT_HANDSHAKING)
                        && s2c.size() == 0 && c2s.size() == 0) {
                    return new Pair(client, server);
                }
                boolean progress = false;
                if (cst == 1) {
                    Object out = TlsRuntime.wrap(Long.valueOf(client),
                            new GoSlice(new byte[0], 0, 0, 0));
                    assertTrue(out != null, "client wrap must not return null in handshake");
                    byte[] b = goBytes(out);
                    if (b.length > 0) {
                        c2s.write(b);
                        progress = true;
                    }
                }
                if (sst == SSLEngineResult.HandshakeStatus.NEED_WRAP) {
                    ByteBuffer dst = ByteBuffer.allocate(17 * 1024);
                    SSLEngineResult res = server.wrap(ByteBuffer.allocate(0), dst);
                    dst.flip();
                    byte[] b = new byte[dst.remaining()];
                    dst.get(b);
                    if (b.length > 0) {
                        s2c.write(b);
                        progress = true;
                    }
                    assertTrue(res.getStatus() != SSLEngineResult.Status.CLOSED,
                            "server wrap closed during handshake");
                }
                if ((cst == 2 || cst == 0) && (s2c.size() > 0
                        || TlsRuntime.hasPending(Long.valueOf(client)) == 1)) {
                    Object out = TlsRuntime.unwrap(Long.valueOf(client),
                            slice(s2c.toByteArray()));
                    s2c.reset();
                    assertTrue(out != null, "client unwrap must not return null in handshake");
                    progress = true;
                }
                if (sst == SSLEngineResult.HandshakeStatus.NEED_UNWRAP && c2s.size() > 0) {
                    ByteBuffer src = ByteBuffer.wrap(c2s.toByteArray());
                    ByteBuffer dst = ByteBuffer.allocate(17 * 1024);
                    SSLEngineResult res = server.unwrap(src, dst);
                    byte[] rest = new byte[src.remaining()];
                    src.get(rest);
                    c2s.reset();
                    c2s.write(rest);
                    assertTrue(res.getStatus() != SSLEngineResult.Status.CLOSED,
                            "server unwrap closed during handshake");
                    progress = true;
                }
                if (cst == 3) {
                    TlsRuntime.runTasks(Long.valueOf(client));
                    progress = true;
                }
                if (sst == SSLEngineResult.HandshakeStatus.NEED_TASK) {
                    Runnable task;
                    while ((task = server.getDelegatedTask()) != null) {
                        task.run();
                    }
                    progress = true;
                }
                assertTrue(progress, "handshake stalled at iteration " + i
                        + " (client status " + cst + ", server status " + sst + ")");
            }
            throw new AssertionError("handshake did not finish in 100 iterations");
        } catch (Throwable t) {
            TlsRuntime.close(Long.valueOf(client));
            throw t;
        }
    }

    public void testAppDataWithCloseNotify() throws Exception {
        Pair p = handshake();
        try {
            // Server flight: one app-data record, then close_notify.
            ByteBuffer dst = ByteBuffer.allocate(17 * 1024);
            SSLEngineResult res = p.server.wrap(
                    ByteBuffer.wrap("final-bytes".getBytes()), dst);
            assertTrue(res.getStatus() == SSLEngineResult.Status.OK,
                    "server app wrap status " + res.getStatus());
            dst.flip();
            byte[] appFlight = new byte[dst.remaining()];
            dst.get(appFlight);
            assertTrue(appFlight.length > 0, "server app flight must be non-empty");
            p.server.closeOutbound();
            dst = ByteBuffer.allocate(17 * 1024);
            res = p.server.wrap(ByteBuffer.allocate(0), dst);
            assertTrue(res.getStatus() == SSLEngineResult.Status.CLOSED,
                    "server close wrap status " + res.getStatus());
            dst.flip();
            byte[] closeFlight = new byte[dst.remaining()];
            dst.get(closeFlight);
            assertTrue(closeFlight.length > 0, "close_notify flight must be non-empty");
            byte[] flight = new byte[appFlight.length + closeFlight.length];
            System.arraycopy(appFlight, 0, flight, 0, appFlight.length);
            System.arraycopy(closeFlight, 0, flight, appFlight.length, closeFlight.length);

            // ONE unwrap call over the combined flight: pre-fix this
            // returned null and "final-bytes" was lost.
            Object out = TlsRuntime.unwrap(Long.valueOf(p.client), slice(flight));
            assertTrue(out != null,
                    "unwrap of app-data + close_notify must return the app bytes, not null");
            assertBytes("final-bytes".getBytes(), goBytes(out),
                    "app bytes produced with close_notify");
            assertEq(TlsRuntime.UNWRAP_CLOSED,
                    TlsRuntime.unwrapState(Long.valueOf(p.client)),
                    "unwrapState after close_notify");
            // Sticky: the next read reports the close without blocking.
            assertEq(TlsRuntime.UNWRAP_CLOSED,
                    TlsRuntime.unwrapState(Long.valueOf(p.client)),
                    "unwrapState must stay CLOSED");
        } finally {
            p.close();
        }
    }

    public void testCorruptRecord() throws Exception {
        Pair p = handshake();
        try {
            // Handshake-type record after the handshake, payload 0xFF:
            // not a valid handshake message, so unwrap must fail.
            byte[] garbage = new byte[5 + 32];
            garbage[0] = 0x16;
            garbage[1] = 0x03;
            garbage[2] = 0x01;
            garbage[3] = 0x00;
            garbage[4] = 0x20;
            java.util.Arrays.fill(garbage, 5, garbage.length, (byte) 0xFF);
            Object out = TlsRuntime.unwrap(Long.valueOf(p.client), slice(garbage));
            assertTrue(out != null,
                    "unwrap of a corrupt record must return bytes (possibly empty), not null");
            assertEq(TlsRuntime.UNWRAP_ERROR,
                    TlsRuntime.unwrapState(Long.valueOf(p.client)),
                    "unwrapState after a corrupt record must be ERROR, not CLOSED/OK");
        } finally {
            p.close();
        }
    }

    public void testTruncatedRecordIsNotTerminal() throws Exception {
        Pair p = handshake();
        try {
            // App-data header claiming 32 payload bytes, only 3 sent:
            // an incomplete record awaiting more bytes. This is neither
            // a close nor an error at the unwrap layer; the error (vs
            // clean EOF) surfaces only when the transport EOFs with
            // these bytes still parked, which the Go side reports as
            // io.ErrUnexpectedEOF.
            byte[] partial = {0x17, 0x03, 0x03, 0x00, 0x20, 0x01, 0x02, 0x03};
            Object out = TlsRuntime.unwrap(Long.valueOf(p.client), slice(partial));
            assertTrue(out != null, "unwrap of a partial record must not return null");
            assertEq(0, goBytes(out).length, "partial record must produce no app bytes");
            assertEq(TlsRuntime.UNWRAP_OK,
                    TlsRuntime.unwrapState(Long.valueOf(p.client)),
                    "unwrapState after a partial record must stay OK");
        } finally {
            p.close();
        }
    }
}
