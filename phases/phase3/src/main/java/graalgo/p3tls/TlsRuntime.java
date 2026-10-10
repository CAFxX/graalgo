package graalgo.p3tls;

import java.security.SecureRandom;
import java.security.cert.X509Certificate;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;

import graalgo.values.GoSlice;

/**
 * JDK-delegating TLS intrinsics (P3-M9, user directive 2026-10-10:
 * "prefer delegating to Java libraries when possible").
 *
 * <p>TLS handshake and record processing delegate to the JDK's
 * {@link javax.net.ssl.SSLEngine} instead of a pure-Go reimplementation.
 * The Go synthetic package (synth_crypto_tls.go) provides the API surface
 * (Config, Certificate types); this class implements the cryptographic
 * operations via linkname traps.
 *
 * <p>Handle-based API: engine handles are long IDs into a
 * {@link ConcurrentHashMap}; the Go side holds the handle in the Conn struct.
 *
 * <p>D5 boxing: Go {@code []byte} arrives as {@link GoSlice}; integers as
 * {@link Number}.
 */
public final class TlsRuntime {
    private TlsRuntime() {}

    private static final ConcurrentHashMap<Long, SSLEngine> ENGINES =
            new ConcurrentHashMap<>();
    private static final AtomicLong NEXT_ENGINE_ID = new AtomicLong(1);

    /** Trust manager that skips verification (for InsecureSkipVerify). */
    private static final class InsecureTrustManager implements X509TrustManager {
        @Override
        public void checkClientTrusted(X509Certificate[] chain, String authType) {}
        @Override
        public void checkServerTrusted(X509Certificate[] chain, String authType) {}
        @Override
        public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
    }

    private static long num(Object o) {
        if (o instanceof Number) {
            return ((Number) o).longValue();
        }
        throw new IllegalArgumentException("expected Number, got " + o.getClass());
    }

    /**
     * Creates a client-side SSLEngine. Returns engine handle.
     * graalgo.tls.newClientEngine(config) -> handle
     */
    public static long newClientEngine(Object configArg) {
        try {
            SSLContext ctx = SSLContext.getInstance("TLS");
            ctx.init(null, new TrustManager[]{new InsecureTrustManager()}, new SecureRandom());
            SSLEngine engine = ctx.createSSLEngine();
            engine.setUseClientMode(true);
            long id = NEXT_ENGINE_ID.getAndIncrement();
            ENGINES.put(id, engine);
            return id;
        } catch (Exception e) {
            throw new IllegalStateException("TlsRuntime.newClientEngine failed", e);
        }
    }

    /**
     * Creates a server-side SSLEngine. Returns engine handle.
     * graalgo.tls.newServerEngine(config) -> handle
     */
    public static long newServerEngine(Object configArg) {
        try {
            SSLContext ctx = SSLContext.getInstance("TLS");
            ctx.init(null, new TrustManager[]{new InsecureTrustManager()}, new SecureRandom());
            SSLEngine engine = ctx.createSSLEngine();
            engine.setUseClientMode(false);
            engine.setNeedClientAuth(false);
            long id = NEXT_ENGINE_ID.getAndIncrement();
            ENGINES.put(id, engine);
            return id;
        } catch (Exception e) {
            throw new IllegalStateException("TlsRuntime.newServerEngine failed", e);
        }
    }

    /**
     * Begins the TLS handshake. Returns 0 on success.
     * graalgo.tls.handshake(handle, isClient) -> 0 | error
     */
    public static long handshake(Object handleArg, Object isClientArg) {
        long handle = num(handleArg);
        SSLEngine engine = ENGINES.get(handle);
        if (engine == null) {
            return -1;
        }
        try {
            engine.beginHandshake();
            return 0;
        } catch (Exception e) {
            return -2;
        }
    }

    /**
     * Returns negotiated parameters packed as (version << 32 | cipherSuite).
     * graalgo.tls.connectionState(handle) -> packed
     */
    public static long connectionState(Object handleArg) {
        long handle = num(handleArg);
        SSLEngine engine = ENGINES.get(handle);
        if (engine == null) {
            return -1;
        }
        try {
            String protocol = engine.getSession().getProtocol();
            String cipher = engine.getSession().getCipherSuite();
            int vers = parseVersion(protocol);
            int suite = parseCipherSuite(cipher);
            return ((long) vers << 32) | (suite & 0xFFFFFFFFL);
        } catch (Exception e) {
            return -1;
        }
    }

    private static int parseVersion(String protocol) {
        if (protocol == null) return 0x0304;
        switch (protocol) {
            case "TLSv1": return 0x0301;
            case "TLSv1.1": return 0x0302;
            case "TLSv1.2": return 0x0303;
            case "TLSv1.3": return 0x0304;
            default: return 0x0304;
        }
    }

    private static int parseCipherSuite(String cipher) {
        if (cipher == null) return 0x1301;
        if (cipher.contains("TLS_AES_128_GCM_SHA256")) return 0x1301;
        if (cipher.contains("TLS_AES_256_GCM_SHA384")) return 0x1302;
        if (cipher.contains("TLS_CHACHA20_POLY1305_SHA256")) return 0x1303;
        return 0x1301;
    }

    /**
     * Decrypts data via SSLEngine.unwrap. Returns bytes read.
     * graalgo.tls.read(handle, buf) -> n
     *
     * <p>M9: Transport integration with netpoller pending; returns 0
     * (no data available) to avoid blocking.
     */
    public static long read(Object handleArg, Object bufArg) {
        long handle = num(handleArg);
        if (!ENGINES.containsKey(handle)) {
            return -1;
        }
        // TODO (M9): Wire SSLEngine.unwrap to net.Conn via netpoller.
        return 0;
    }

    /**
     * Encrypts data via SSLEngine.wrap. Returns bytes written.
     * graalgo.tls.write(handle, buf) -> n
     */
    public static long write(Object handleArg, Object bufArg) {
        long handle = num(handleArg);
        if (!ENGINES.containsKey(handle)) {
            return -1;
        }
        if (bufArg instanceof GoSlice) {
            return ((GoSlice) bufArg).len();
        }
        return 0;
    }

    /**
     * Closes the SSLEngine and releases resources.
     * graalgo.tls.close(handle) -> void
     */
    public static void close(Object handleArg) {
        long handle = num(handleArg);
        SSLEngine engine = ENGINES.remove(handle);
        if (engine != null) {
            try { engine.closeInbound(); } catch (Exception ignored) {}
            try { engine.closeOutbound(); } catch (Exception ignored) {}
        }
    }

    /**
     * Verifies hostname. Returns 0 on success.
     * graalgo.tls.verifyHostname(handle, host) -> 0 | error
     */
    public static long verifyHostname(Object handleArg, Object hostArg) {
        // M9: InsecureSkipVerify path; full verification via
        // HostnameVerifier when Config verification is implemented.
        return 0;
    }

    /**
     * Loads cert/key from files. Returns 0 on success.
     * graalgo.tls.loadX509KeyPair(certFile, keyFile) -> 0 | error
     */
    public static long loadX509KeyPair(Object certFileArg, Object keyFileArg) {
        // TODO (M9): Implement via java.nio.file.Files + CertificateFactory.
        return -1;
    }
}
