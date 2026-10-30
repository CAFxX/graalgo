package graalgo.p3tls;

import java.io.ByteArrayOutputStream;
import java.lang.reflect.Field;
import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Base64;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;

import javax.net.ssl.SSLEngine;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Tests for TlsRuntime engine configuration: SNI omission for IP
 * literals and unusable hostnames (review N-P1-8), ALPN offer and
 * selection, and server client-auth modes (review N-P1-9).
 *
 * <p>Server fixture: the graalgo-test-CA server certificate and its
 * matching RSA key from test/corpus/wave3/crypto/tls/01_handshake.go
 * (PEM bodies embedded here so this test is self-contained; the cert
 * is only parsed by CertificateFactory and the key only feeds
 * buildPrivateKey, so expiry does not matter). Flag assertions use
 * reflection into TlsRuntime's private engine table: they pin engine
 * configuration, not handshake outcomes. The handshake pumps below
 * assert the same properties behaviorally through the public trap
 * surface (wrap/unwrap/engineStatus), exactly as the Go driving loop
 * uses it.
 */
public final class TlsRuntimeTest {

    // Server certificate (DER, base64) from the corpus handshake fixture.
    private static final String SRV_CERT_B64 = "MIIDGjCCAgKgAwIBAgIBAjANBgkqhkiG9w0BAQsFADAaMRgwFgYDVQQDEw9ncmFhbGdvLXRlc3QtY2EwHhcNMjAwMTAxMDAwMDAwWhcNMzAwMTAxMDAwMDAwWjAUMRIwEAYDVQQDEwlsb2NhbGhvc3QwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCt1dUUe3CCvA2lW3k9CTei512AP2p+IUEDuoaxyi+cEwEFQKtqE8/VS59lD3KBZBc1Vls20uf3LmsMX/fI062SEZm+D1KQrGj4azDkUFbtGGGRV+ujmBdE6SBomWnbAJrF9CeAXIJT1QC/JUqydFXiZt/TUN6+h8pG8+Fb2tbDLGRV86ZlemW66Ro1W7jmuFQOvlbVdoas2DLWHLZU5RM7U0qtjshCEOt5kxXpayxRMm+ax3MGlogUkX4gwczMjB6msEQwuFs7rvog+shHysioF1Q6U/Q6fejSSm6IKTqrCf2RDUHDCXHyCMQ4DLBelqmkI9v/SuW4124vRIqsAD3pAgMBAAGjcTBvMA4GA1UdDwEB/wQEAwIHgDATBgNVHSUEDDAKBggrBgEFBQcDATAfBgNVHSMEGDAWgBQjW9fRc89jeQWJUkgdXjdiFTrFNDAnBgNVHREEIDAegglsb2NhbGhvc3SCC2V4YW1wbGUuY29thwR/AAABMA0GCSqGSIb3DQEBCwUAA4IBAQCv149c0cHrbjaiJMnTrTKzloTMZVaqbcBIdmZDKbz0WFmnEXW3eQy4KCC5II3rdOf3jDAdE8LWP6yfshDrIoMy/Ze1r896u8yE/LlzDxyxjr+QLYv4snQRJk72bAYeFAwpwLVulLYlZh29V+DNWL6a/+4VcStQLJ8adRA4RqtgMesp3mE+/POgpwCmz5kP1+rA1noaWh2PslS4k6Igvn60WsZ3vVCX37J9js6ML3ZxhadnnyvcveOSGnYVN3vN/ZqzUUFGRGZDkC0lEcSb6+qXZIQOVGfTT4j5PmQEfImfmpAXmS98pvkmVmYJJFgkLNz9bVYiKxbYq3leFurZFGUO";

    // Server RSA private key (PKCS#1 DER, base64) from the same fixture.
    private static final String SRV_KEY_B64 = "MIIEogIBAAKCAQEArdXVFHtwgrwNpVt5PQk3ouddgD9qfiFBA7qGscovnBMBBUCrahPP1UufZQ9ygWQXNVZbNtLn9y5rDF/3yNOtkhGZvg9SkKxo+Gsw5FBW7RhhkVfro5gXROkgaJlp2wCaxfQngFyCU9UAvyVKsnRV4mbf01DevofKRvPhW9rWwyxkVfOmZXpluukaNVu45rhUDr5W1XaGrNgy1hy2VOUTO1NKrY7IQhDreZMV6WssUTJvmsdzBpaIFJF+IMHMzIweprBEMLhbO676IPrIR8rIqBdUOlP0On3o0kpuiCk6qwn9kQ1Bwwlx8gjEOAywXpappCPb/0rluNduL0SKrAA96QIDAQABAoIBAChrdIi3LRGQ9WFSFC4vgvrDblfAJxxd0hayW9hiKVDSkGWuxEy9f5FabJ0ciuTwJ39U7Y+hWaN9bRGrDNvq8ExrAEMToACYvi2pCuzDi3rEtQcNGPLojdyNkmYP6e+sw9AsdbTgjcM9X1eSrbwx1bh3cCrb/nbx7wQxd+CaUV5/BUyU36l9LShtpigsonjL0P0Ew+fm79bOZpOeap1zBWkhnhnOSA4eRynqpXak3KO1HIFgh1T45e9q3yXwUV/PLKLNkEXCRAWaOiHhxBllZU+ewP/9eSiRvz9e+TVfbk/phbBRm37MwBDdZttSBPrVa99cHxZfj+4Rd1VKsbPyYeMCgYEA2b9y7gn/q5HDKItIZuNdLx1aj49JHyFWtf0PgupFUhksDPg8nGp9wSWFGUd5TaRLqod9ducEbOQRdJzUDOo9fkzd8lklqdMtF/0qECJR8F16FLye4ajfXc+4vv+Me3Hv+l5+r1af6AcBO8vHDkM7CS1aF3rLPsVPU5IcQEumklcCgYEAzF+Mbwm6Inen4POnEepjFfGWS8LQ3lSb9zt2pdC4liYxbznr/YTddOX2Qm4LpwJH7S0TZCVpwVcTPf3voDb3uSOUspoi670OKgGuKbePoUfzT2NBwgXMV3eOtfhvqYmXT22aNXKqfPJnrEtuUzoem3yTYUY4XdZi3Gro0miMCb8CgYBvVZFJHrmoGtZjU5wIZ3ZM0cBtKMEldqFkWejlci1RheddFku/Wj/EUw/T1hSy2y+JGM/QzNwZZR97tXhCxj385waQlH5Kb9xdqKcqKrIkqVbzZWIDVZon2D7JAdbtg1tFc61or20BcRtzXIUByORVk+e9m55yEOa3fzyMr2GS7wKBgBhce1zxB+L76GMEKDIWgmEnPwcg3cziwQSjbe0uCBK90r6zZNH/eHZqwwQAIhtyRiibauhRHV4ShxSopFuwsa678wmnHeqDyqBvOtd2LgTHktUvu7LpBgaXFcfTKVxILmUlZ3Bfjrr+GiWLiEcbcnKOUvMRb76fw4EKrV4HYEn5AoGAI5nb8K9GaCSoPKVojgUezQHpmJ55x1S+XGAx2BjZphZj27OvqNE7NB9RO2twfPCYDMvTQiE8cmquId60jxYnCWblSoW2qEWzVizIKuCbBjuGswdgqAWp8IX0C5baQ4MR6V6bahfKjdlUCWpAhRpl8DgJ/k/hVMPWltreAOlSN9Y=";

    // Go crypto/tls ClientAuthType values (synth_crypto_tls.go).
    private static final long NO_CLIENT_CERT = 0;
    private static final long REQUEST_CLIENT_CERT = 1;
    private static final long REQUIRE_ANY_CLIENT_CERT = 2;
    private static final long VERIFY_CLIENT_CERT_IF_GIVEN = 3;
    private static final long REQUIRE_AND_VERIFY_CLIENT_CERT = 4;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(String want, String got, String msg) {
        if (!want.equals(got)) {
            throw new AssertionError(msg + ": want <" + want + "> got <" + got + ">");
        }
    }

    private static Object goBytes(byte[] b) {
        return new GoSlice(b, 0, b.length, b.length);
    }

    private static byte[] sliceBytes(Object o) {
        if (o == null) {
            return null;
        }
        GoSlice gs = (GoSlice) o;
        byte[] arr = (byte[]) gs.array();
        return Arrays.copyOfRange(arr, gs.offset(), gs.offset() + gs.len());
    }

    /** Length-prefixed framing (Go encodeLengthPrefixed / encodeNextProtos). */
    private static byte[] lengthPrefixed(byte[][] items) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        for (byte[] item : items) {
            int n = item.length;
            out.write((n >>> 24) & 0xFF);
            out.write((n >>> 16) & 0xFF);
            out.write((n >>> 8) & 0xFF);
            out.write(n & 0xFF);
            out.write(item, 0, item.length);
        }
        return out.toByteArray();
    }

    private static byte[] encodeProtos(String[] protos) {
        byte[][] items = new byte[protos.length][];
        for (int i = 0; i < protos.length; i++) {
            items[i] = protos[i].getBytes(java.nio.charset.StandardCharsets.UTF_8);
        }
        return lengthPrefixed(items);
    }

    /** Minimal DER reader: SEQUENCE + INTEGERs of a PKCS#1 RSA key. */
    private static final class DerReader {
        private final byte[] b;
        private int p;

        DerReader(byte[] b) {
            this.b = b;
        }

        int tag() {
            return b[p++] & 0xFF;
        }

        int length() {
            int l = b[p++] & 0xFF;
            if (l < 0x80) {
                return l;
            }
            int n = l & 0x7F;
            int v = 0;
            for (int i = 0; i < n; i++) {
                v = (v << 8) | (b[p++] & 0xFF);
            }
            return v;
        }

        BigInteger integer() {
            if (tag() != 0x02) {
                throw new AssertionError("expected INTEGER in PKCS#1 key");
            }
            int len = length();
            byte[] v = Arrays.copyOfRange(b, p, p + len);
            p += len;
            return new BigInteger(1, v);
        }
    }

    private static byte[] srvCertDer() {
        return Base64.getDecoder().decode(SRV_CERT_B64);
    }

    /** RSA keyParams for TlsRuntime: length-prefixed n, e, d, p, q. */
    private static byte[] srvKeyParams() {
        byte[] der = Base64.getDecoder().decode(SRV_KEY_B64);
        DerReader r = new DerReader(der);
        if (r.tag() != 0x30) {
            throw new AssertionError("expected SEQUENCE in PKCS#1 key");
        }
        r.length();
        BigInteger[] fields = new BigInteger[9];
        for (int i = 0; i < 9; i++) {
            fields[i] = r.integer();
        }
        // PKCS#1 order: version, n, e, d, p, q, dp, dq, qinv.
        byte[][] params = new byte[5][];
        for (int i = 0; i < 5; i++) {
            params[i] = fields[i + 1].toByteArray();
        }
        return lengthPrefixed(params);
    }

    private static long newClient(String serverName, String[] protos) {
        return TlsRuntime.newClientEngine(GoString.fromUtf8(serverName),
                Long.valueOf(0), Long.valueOf(0), Long.valueOf(0),
                goBytes(new byte[0]), goBytes(encodeProtos(protos)));
    }

    private static long newServer(String[] protos, long clientAuth) {
        return TlsRuntime.newServerEngine(
                goBytes(lengthPrefixed(new byte[][]{srvCertDer()})),
                GoString.fromUtf8("RSA"),
                goBytes(srvKeyParams()),
                Long.valueOf(0), Long.valueOf(0),
                goBytes(new byte[0]), goBytes(encodeProtos(protos)),
                Long.valueOf(clientAuth));
    }

    @SuppressWarnings("unchecked")
    private static SSLEngine engineFor(long handle) throws Exception {
        Field f = TlsRuntime.class.getDeclaredField("ENGINES");
        f.setAccessible(true);
        ConcurrentHashMap<Long, SSLEngine> map =
                (ConcurrentHashMap<Long, SSLEngine>) f.get(null);
        SSLEngine e = map.get(handle);
        if (e == null) {
            throw new AssertionError("no engine for handle " + handle);
        }
        return e;
    }

    private static String negotiated(long handle) {
        byte[] b = sliceBytes(TlsRuntime.negotiatedProtocol(handle));
        if (b == null) {
            return "";
        }
        return new String(b, java.nio.charset.StandardCharsets.UTF_8);
    }

    /** Engine status, with a failed engine reported as -1, never thrown. */
    private static long status(long handle) {
        try {
            return TlsRuntime.engineStatus(handle);
        } catch (Exception e) {
            return -1;
        }
    }

    private static byte[] concat(byte[] a, byte[] b) {
        byte[] out = Arrays.copyOf(a, a.length + b.length);
        System.arraycopy(b, 0, out, a.length, b.length);
        return out;
    }

    /** Whether the engine's session is valid (false after a failed handshake). */
    private static boolean sessionValid(long handle) throws Exception {
        return engineFor(handle).getSession().isValid();
    }

    /**
     * Drives both engines through the public trap surface until both
     * report FINISHED or maxIters passes. Returns true iff both
     * finished. Mirrors the Go driving loop: beginHandshake first
     * (without it the engines report NOT_HANDSHAKING, which the
     * status mapping cannot distinguish from FINISHED, so a pump
     * that skipped it would "succeed" without any handshake), then
     * wrap on NEED_WRAP, unwrap on NEED_UNWRAP (draining Java-side
     * pending bytes first), runTasks on NEED_TASK.
     */
    private static boolean pump(long client, long server, int maxIters) {
        if (TlsRuntime.beginHandshake(client) != 0
                || TlsRuntime.beginHandshake(server) != 0) {
            return false;
        }
        byte[] c2s = new byte[0];
        byte[] s2c = new byte[0];
        for (int i = 0; i < maxIters; i++) {
            long cs = status(client);
            long ss = status(server);
            if (cs == 0 && ss == 0) {
                return true;
            }
            if (cs == 1) {
                byte[] out = sliceBytes(TlsRuntime.wrap(client, goBytes(new byte[0])));
                if (out != null && out.length > 0) {
                    c2s = concat(c2s, out);
                }
            } else if (cs == 2) {
                if (s2c.length > 0) {
                    TlsRuntime.unwrap(client, goBytes(s2c));
                    s2c = new byte[0];
                } else if (TlsRuntime.hasPending(client) != 0) {
                    TlsRuntime.unwrap(client, goBytes(new byte[0]));
                }
            } else if (cs == 3) {
                TlsRuntime.runTasks(client);
            }
            if (ss == 1) {
                byte[] out = sliceBytes(TlsRuntime.wrap(server, goBytes(new byte[0])));
                if (out != null && out.length > 0) {
                    s2c = concat(s2c, out);
                }
            } else if (ss == 2) {
                if (c2s.length > 0) {
                    TlsRuntime.unwrap(server, goBytes(c2s));
                    c2s = new byte[0];
                } else if (TlsRuntime.hasPending(server) != 0) {
                    TlsRuntime.unwrap(server, goBytes(new byte[0]));
                }
            } else if (ss == 3) {
                TlsRuntime.runTasks(server);
            }
        }
        return status(client) == 0 && status(server) == 0;
    }

    public void testSniSkippedForIpLiteral() {
        // Before the fix these threw IllegalStateException (SNIHostName
        // rejects IP literals, and the catch rethrew as a guest crash).
        for (String ip : new String[]{"127.0.0.1", "::1", "192.168.1.10",
                "2001:db8::1", "1:2:3:4:5:6:1.2.3.4"}) {
            long h = newClient(ip, new String[0]);
            assertTrue(h > 0, "engine handle for IP literal " + ip);
            TlsRuntime.close(h);
        }
    }

    public void testSniSkippedForUnderscoreHostname() {
        long h = newClient("bad_host.local", new String[0]);
        assertTrue(h > 0, "engine handle for underscore hostname");
        TlsRuntime.close(h);
    }

    public void testSniPresentForHostname() throws Exception {
        long h = newClient("example.com", new String[0]);
        try {
            SSLEngine e = engineFor(h);
            List<javax.net.ssl.SNIServerName> names =
                    e.getSSLParameters().getServerNames();
            assertTrue(names != null && names.size() == 1,
                    "exactly one SNI name for example.com, got " + names);
            assertEq("example.com",
                    ((javax.net.ssl.SNIHostName) names.get(0)).getAsciiName(),
                    "SNI hostname");
        } finally {
            TlsRuntime.close(h);
        }
    }

    public void testSniAbsentForIpLiteral() throws Exception {
        long h = newClient("127.0.0.1", new String[0]);
        try {
            SSLEngine e = engineFor(h);
            List<javax.net.ssl.SNIServerName> names =
                    e.getSSLParameters().getServerNames();
            assertTrue(names == null || names.isEmpty(),
                    "no SNI for IP literal, got " + names);
        } finally {
            TlsRuntime.close(h);
        }
    }

    public void testClientAlpnOffered() throws Exception {
        long h = newClient("example.com", new String[]{"h2", "http/1.1"});
        try {
            String[] offered = engineFor(h).getSSLParameters().getApplicationProtocols();
            assertTrue(offered != null && offered.length == 2
                            && "h2".equals(offered[0]) && "http/1.1".equals(offered[1]),
                    "client ALPN offer, got " + Arrays.toString(offered));
        } finally {
            TlsRuntime.close(h);
        }
    }

    public void testServerAlpnAndClientAuthFlags() throws Exception {
        long[][] cases = {
                // {clientAuth, want(1/0), need(1/0)}
                {NO_CLIENT_CERT, 0, 0},
                {REQUEST_CLIENT_CERT, 1, 0},
                {REQUIRE_ANY_CLIENT_CERT, 0, 1},
                {VERIFY_CLIENT_CERT_IF_GIVEN, 1, 0},
                {REQUIRE_AND_VERIFY_CLIENT_CERT, 0, 1},
        };
        for (long[] c : cases) {
            long h = newServer(new String[]{"h2"}, c[0]);
            try {
                SSLEngine e = engineFor(h);
                assertTrue(e.getWantClientAuth() == (c[1] == 1),
                        "wantClientAuth for auth " + c[0]);
                assertTrue(e.getNeedClientAuth() == (c[2] == 1),
                        "needClientAuth for auth " + c[0]);
                String[] offered = e.getSSLParameters().getApplicationProtocols();
                assertTrue(offered != null && offered.length == 1
                                && "h2".equals(offered[0]),
                        "server ALPN offer for auth " + c[0]);
            } finally {
                TlsRuntime.close(h);
            }
        }
    }

    public void testAlpnNegotiatedInHandshake() throws Exception {
        long client = newClient("localhost", new String[]{"h2", "http/1.1"});
        long server = newServer(new String[]{"h2"}, NO_CLIENT_CERT);
        try {
            assertTrue(pump(client, server, 200), "ALPN handshake completes");
            assertTrue(sessionValid(client) && sessionValid(server),
                    "both sessions valid after handshake");
            assertEq("h2", negotiated(client), "client negotiated protocol");
            assertEq("h2", negotiated(server), "server negotiated protocol");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }

    public void testIpLiteralHandshakeSucceeds() throws Exception {
        long client = newClient("127.0.0.1", new String[0]);
        long server = newServer(new String[0], NO_CLIENT_CERT);
        try {
            assertTrue(pump(client, server, 200),
                    "handshake to IP-literal server completes");
            assertTrue(sessionValid(client) && sessionValid(server),
                    "both sessions valid after IP-literal handshake");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }

    public void testRequireAndVerifyRejectsUnauthenticatedClient() throws Exception {
        long client = newClient("localhost", new String[0]);
        long server = newServer(new String[0], REQUIRE_AND_VERIFY_CLIENT_CERT);
        try {
            // The pump may report the client finished (in TLS 1.3 the
            // client completes before the server validates its
            // certificate) or may stall; either way the server's
            // session must be invalid: the handshake did not succeed.
            pump(client, server, 200);
            assertTrue(!sessionValid(server),
                    "server session must be invalid without a client "
                            + "certificate under RequireAndVerifyClientCert");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }

    public void testRequestClientCertAllowsUnauthenticatedClient() throws Exception {
        long client = newClient("localhost", new String[0]);
        long server = newServer(new String[0], REQUEST_CLIENT_CERT);
        try {
            assertTrue(pump(client, server, 200),
                    "handshake without a client certificate completes "
                            + "under RequestClientCert");
            assertTrue(sessionValid(client) && sessionValid(server),
                    "both sessions valid under RequestClientCert");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }

    public void testNegotiatedProtocolEmptyWithoutAlpn() throws Exception {
        long client = newClient("localhost", new String[0]);
        long server = newServer(new String[0], NO_CLIENT_CERT);
        try {
            assertTrue(pump(client, server, 200), "plain handshake completes");
            assertTrue(sessionValid(client) && sessionValid(server),
                    "both sessions valid after plain handshake");
            assertEq("", negotiated(client), "no ALPN means empty protocol");
            assertEq("", negotiated(0), "unknown handle means empty protocol");
        } finally {
            TlsRuntime.close(client);
            TlsRuntime.close(server);
        }
    }
}
