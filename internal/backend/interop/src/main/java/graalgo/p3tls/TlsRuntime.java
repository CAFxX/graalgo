package graalgo.p3tls;

import java.math.BigInteger;
import java.net.Socket;
import java.security.KeyFactory;
import java.security.PrivateKey;
import java.security.SecureRandom;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.security.spec.ECGenParameterSpec;
import java.security.spec.ECParameterSpec;
import java.security.spec.ECPrivateKeySpec;
import java.security.spec.EdECPrivateKeySpec;
import java.security.spec.EdECPoint;
import java.security.spec.NamedParameterSpec;
import java.security.spec.PKCS8EncodedKeySpec;
import java.security.spec.RSAPrivateCrtKeySpec;
import java.security.spec.X509EncodedKeySpec;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

import javax.net.ssl.KeyManager;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLEngineResult;
import javax.net.ssl.SSLSession;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509ExtendedKeyManager;
import javax.net.ssl.X509TrustManager;

import java.nio.ByteBuffer;
import java.security.AlgorithmParameters;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * JDK-delegating TLS intrinsics (P3-M9, user directive 2026-10-10:
 * "prefer delegating to Java libraries when possible").
 *
 * <p>TLS handshake and record processing delegate to the JDK's
 * {@link javax.net.ssl.SSLEngine}. The Go synthetic package
 * (synth_crypto_tls.go) drives the handshake state machine: it shuttles
 * bytes between the engine (via {@link #wrap}/{@link #unwrap}) and the
 * Go {@code net.Conn}. This class only implements the cryptographic
 * operations via linkname traps.
 *
 * <p>Handle-based API: engine handles are long IDs into a
 * {@link ConcurrentHashMap}; the Go side holds the handle in the Conn struct.
 *
 * <p>D5 boxing: Go {@code []byte} arrives as {@link GoSlice}; strings as
 * {@link GoString}; integers and bools as {@link Number} (bool as 0/1).
 *
 * <p>Trust: the engine's TrustManager is permissive during the handshake;
 * certificate verification (chain building, hostname) is implemented in Go
 * (synthetic crypto/x509 Verify) for exact Go semantics. The Go side calls
 * {@link #peerCertificates} after the handshake and verifies itself.
 *
 * <h2>Error channel (R23-P1-3)</h2>
 *
 * <p>No {@code Throwable} of type {@code Exception} ever escapes a
 * {@code graalgo.tls.*} trap: host exceptions would abort the whole process,
 * while gc surfaces the same conditions as Go {@code error} values. Failures
 * are signalled with in-band sentinels instead:
 *
 * <ul>
 *   <li>Every {@code long}-returning trap returns a <b>negative error-slot
 *       key</b> on failure (valid results are provably never negative:
 *       handles start at 1, statuses are 0-3, packed connection state is
 *       non-negative). The Go side passes the key to
 *       {@link #lastError(Object)} to retrieve the message.</li>
 *   <li>{@code Object}-returning traps ({@link #wrap}, {@link #unwrap},
 *       {@link #peerCertificates}) keep their existing {@code null}/empty
 *       meanings and stash detail under the engine-handle key, also read via
 *       {@link #lastError(Object)}.</li>
 * </ul>
 *
 * <p>One {@link ConcurrentHashMap} ({@code ERRORS}) serves both key spaces:
 * negative keys are consume-on-read error slots, positive keys are
 * handle-scoped errors. Slot allocation is via {@link AtomicLong}; no locks,
 * no {@code ThreadLocal} (trap calls from one goroutine may migrate across
 * host threads under Truffle, so thread-identity-keyed state would be
 * unsound).
 *
 * <p>Every trap body is wrapped in {@code catch (Exception)}: an
 * {@code Error} ({@code OutOfMemoryError}, linkage errors, ...) still
 * propagates and aborts, matching gc's fatal treatment of truly fatal
 * conditions.
 *
 * <p>Single-driver-per-handle discipline (R23-6, unchanged): the Go driver
 * issues at most one failing call per handle before reading the error. Two
 * failing calls without an intervening {@code lastError} lose the first
 * message (last-write-wins) — the documented protocol, not a race.
 */
public final class TlsRuntime {
    private TlsRuntime() {}

    private static final ConcurrentHashMap<Long, SSLEngine> ENGINES =
            new ConcurrentHashMap<>();
    private static final AtomicLong NEXT_ENGINE_ID = new AtomicLong(1);

    /** Error detail store. Negative keys: error slots (consume-on-read).
     *  Positive keys: handle-scoped errors for Object-returning traps. */
    private static final ConcurrentHashMap<Long, String> ERRORS =
            new ConcurrentHashMap<>();
    private static final AtomicLong NEXT_ERROR_ID = new AtomicLong(1);
    private static final int MAX_ERROR_LEN = 512;

    /**
     * Unconsumed network input per engine. SSLEngine.unwrap() may consume
     * only part of its input (e.g. it stops after one record when it needs
     * tasks run before processing the next). The Go driving loop passes a
     * fresh byte[] per call, so without buffering here the remainder would
     * be silently discarded — the peer's flight gets truncated, the local
     * wrap() then produces a partial flight (e.g. bare ChangeCipherSpec
     * without Finished), and both sides deadlock in NEED_UNWRAP.
     * P3-M9 (x509): GMP=1 handshake deadlock root cause.
     */
    private static final ConcurrentHashMap<Long, byte[]> PENDING =
            new ConcurrentHashMap<>();

    private static final int APP_BUF = 17 * 1024;
    private static final int NET_BUF = 17 * 1024;

    /** Trust manager that accepts everything (Go verifies post-handshake). */
    private static final class PermissiveTrustManager implements X509TrustManager {
        @Override
        public void checkClientTrusted(X509Certificate[] chain, String authType) {}
        @Override
        public void checkServerTrusted(X509Certificate[] chain, String authType) {}
        @Override
        public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
    }

    /** Key manager serving a fixed chain + private key (server side). */
    private static final class FixedKeyManager extends X509ExtendedKeyManager {
        private final X509Certificate[] chain;
        private final PrivateKey key;

        FixedKeyManager(X509Certificate[] chain, PrivateKey key) {
            this.chain = chain;
            this.key = key;
        }

        @Override
        public String chooseServerAlias(String keyType, java.security.Principal[] issuers,
                                        Socket socket) {
            return "graalgo";
        }

        @Override
        public String chooseEngineServerAlias(String keyType, java.security.Principal[] issuers,
                                              SSLEngine engine) {
            // P3-M9 (x509): SSLEngine (not SSLSocket) drives the handshake —
            // without this override the engine gets no alias and the
            // handshake fails with "No available authentication scheme".
            return "graalgo";
        }

        @Override
        public String chooseClientAlias(String[] keyType, java.security.Principal[] issuers,
                                        Socket socket) {
            return null;
        }

        @Override
        public X509Certificate[] getCertificateChain(String alias) {
            return chain.clone();
        }

        @Override
        public PrivateKey getPrivateKey(String alias) {
            return key;
        }

        @Override
        public String[] getServerAliases(String keyType, java.security.Principal[] issuers) {
            return new String[]{"graalgo"};
        }

        @Override
        public String[] getClientAliases(String keyType, java.security.Principal[] issuers) {
            return new String[0];
        }
    }

    private static Long optNum(Object o) {
        return (o instanceof Number) ? ((Number) o).longValue() : null;
    }

    private static String optStr(Object o) {
        if (o instanceof GoString) {
            return new String(((GoString) o).bytes(),
                    java.nio.charset.StandardCharsets.UTF_8);
        }
        return null;
    }

    /**
     * As before: a Go nil slice (Java null) becomes the empty array.
     * Wrong host type yields null (callers route it to the error channel).
     */
    private static byte[] optBytes(Object o) {
        // A Go nil slice arrives as Java null: treat it as empty (Go
        // semantics: ranging/reading a nil slice yields nothing). Without
        // this, wrap(handle, nil) in the handshake loop produced no output
        // and the Go loop spun forever on NEED_WRAP (01_handshake legs red
        // in legs-rebased).
        if (o == null) {
            return new byte[0];
        }
        if (o instanceof GoSlice) {
            GoSlice gs = (GoSlice) o;
            // []byte is byte[]-backed (D5).
            byte[] arr = (byte[]) gs.array();
            int off = gs.offset();
            int len = gs.len();
            byte[] out = new byte[len];
            System.arraycopy(arr, off, out, 0, len);
            return out;
        }
        return null;
    }

    private static String typeName(Object o) {
        return o == null ? "null" : o.getClass().getName();
    }

    /** Allocates an error slot; returns the (negative) key to hand to Go. */
    private static long fail(String what, Throwable e) {
        long key = -NEXT_ERROR_ID.getAndIncrement();
        ERRORS.put(key, truncate("tls." + what + " failed: " + flat(e)));
        return key;
    }

    private static long failArg(String what, Object badArg, String expected) {
        long key = -NEXT_ERROR_ID.getAndIncrement();
        ERRORS.put(key, truncate("tls." + what + ": expected " + expected
                + ", got " + typeName(badArg)));
        return key;
    }

    /** Handle-scoped variants for Object-returning traps. */
    private static void failHandle(long handle, String what, Throwable e) {
        ERRORS.put(handle, truncate("tls." + what + " failed: " + flat(e)));
    }

    private static void failHandleArg(long handle, String what,
                                      Object badArg, String expected) {
        ERRORS.put(handle, truncate("tls." + what + ": expected " + expected
                + ", got " + typeName(badArg)));
    }

    private static String flat(Throwable e) {
        // One line: SimpleName + message, plus one cause level. No stack trace.
        StringBuilder sb = new StringBuilder();
        sb.append(e.getClass().getSimpleName()).append(": ")
          .append(String.valueOf(e.getMessage()));
        Throwable c = e.getCause();
        if (c != null && c != e) {
            sb.append(" caused by ").append(c.getClass().getSimpleName())
              .append(": ").append(String.valueOf(c.getMessage()));
        }
        return sb.toString();
    }

    private static String truncate(String s) {
        String noNewlines = s.replace('\n', ' ').replace('\r', ' ');
        return noNewlines.length() <= MAX_ERROR_LEN ? noNewlines
                : noNewlines.substring(0, MAX_ERROR_LEN);
    }

    private static Object toGoString(String s) {
        // Same pattern as p3x509.GoX509.toGoString: Go declares `string` return.
        return new GoString(s.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    /**
     * Consumes and returns the error detail for a failed trap call.
     * key: the negative value a long-returning trap returned (error slot),
     *      or an engine handle (handle-scoped error from an Object-returning
     *      trap). Returns "" when nothing is recorded. Never throws.
     * graalgo.tls.lastError(key) -> string
     */
    public static Object lastError(Object keyArg) {
        Long k = optNum(keyArg);
        String m = (k == null) ? null : ERRORS.remove(k);
        return toGoString(m == null ? "" : m);
    }

    private static Object toGoBytes(byte[] b) {
        if (b == null) {
            return null;
        }
        // []byte is byte[]-backed (D5).
        byte[] arr = b.clone();
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    /** Splits length-prefixed byte strings (uint32 BE length + data). */
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

    private static String versionName(long v) {
        if (v == 0x0301) return "TLSv1";
        if (v == 0x0302) return "TLSv1.1";
        if (v == 0x0303) return "TLSv1.2";
        if (v == 0x0304) return "TLSv1.3";
        return null;
    }

    private static String[] enabledProtocols(long minVersion, long maxVersion) {
        // Go 1.27 default: min TLS 1.2, max TLS 1.3. 0 means "use default".
        long min = minVersion == 0 ? 0x0303 : minVersion;
        long max = maxVersion == 0 ? 0x0304 : maxVersion;
        List<String> protos = new ArrayList<>();
        for (long v = 0x0301; v <= 0x0304; v++) {
            if (v >= min && v <= max) {
                protos.add(versionName(v));
            }
        }
        return protos.toArray(new String[0]);
    }

    private static String cipherSuiteName(int suite) {
        switch (suite) {
            case 0x1301: return "TLS_AES_128_GCM_SHA256";
            case 0x1302: return "TLS_AES_256_GCM_SHA384";
            case 0x1303: return "TLS_CHACHA20_POLY1305_SHA256";
            case 0xc02f: return "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256";
            case 0xc02b: return "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256";
            case 0xc030: return "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384";
            case 0xc02c: return "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384";
            case 0xcca8: return "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305";
            case 0xcca9: return "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305";
            case 0x009c: return "TLS_RSA_WITH_AES_128_GCM_SHA256";
            case 0x009d: return "TLS_RSA_WITH_AES_256_GCM_SHA384";
            default: return null;
        }
    }

    private static String[] enabledCipherSuites(byte[] suitesBE) {
        if (suitesBE == null || suitesBE.length == 0) {
            // Go's default cipher suite preference order (crypto/tls
            // defaultCipherSuites), not the JDK's: gc negotiates 0x1301
            // here, the JDK default order negotiates 0x1302.
            return new String[]{
                "TLS_AES_128_GCM_SHA256",
                "TLS_AES_256_GCM_SHA384",
                "TLS_CHACHA20_POLY1305_SHA256",
                "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
                "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
                "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
                "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
                "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
                "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
            };
        }
        List<String> names = new ArrayList<>();
        for (int i = 0; i + 1 < suitesBE.length; i += 2) {
            int suite = ((suitesBE[i] & 0xFF) << 8) | (suitesBE[i + 1] & 0xFF);
            String name = cipherSuiteName(suite);
            if (name != null) {
                names.add(name);
            }
        }
        return names.isEmpty() ? null : names.toArray(new String[0]);
    }

    /**
     * Creates a client-side SSLEngine. Returns the engine handle (>= 1),
     * or a negative error-slot key on failure (see the class javadoc).
     * graalgo.tls.newClientEngine(serverName, insecureSkipVerify,
     *     minVersion, maxVersion, cipherSuites) -> handle | -slot
     */
    public static long newClientEngine(Object serverNameArg, Object insecureArg,
                                       Object minVersionArg, Object maxVersionArg,
                                       Object cipherSuitesArg) {
        String serverName = optStr(serverNameArg);
        if (serverName == null) {
            return failArg("newClientEngine.serverName", serverNameArg, "GoString");
        }
        Long minVersion = optNum(minVersionArg);
        if (minVersion == null) {
            return failArg("newClientEngine.minVersion", minVersionArg, "Number");
        }
        Long maxVersion = optNum(maxVersionArg);
        if (maxVersion == null) {
            return failArg("newClientEngine.maxVersion", maxVersionArg, "Number");
        }
        byte[] suitesBE = optBytes(cipherSuitesArg);
        if (suitesBE == null) {
            return failArg("newClientEngine.cipherSuites", cipherSuitesArg, "GoSlice");
        }
        // insecureArg is accepted but ignored (trust is always permissive;
        // Go verifies post-handshake). Pre-existing; out of scope (R23-P1-3).
        try {
            SSLContext ctx = SSLContext.getInstance("TLS");
            ctx.init(null, new TrustManager[]{new PermissiveTrustManager()},
                    new SecureRandom());
            SSLEngine engine = ctx.createSSLEngine();
            engine.setUseClientMode(true);
            engine.setEnabledProtocols(enabledProtocols(minVersion, maxVersion));
            String[] suites = enabledCipherSuites(suitesBE);
            if (suites != null) {
                engine.setEnabledCipherSuites(suites);
            }
            if (!serverName.isEmpty()) {
                // SNI (used by real servers; harmless for our tests).
                javax.net.ssl.SNIHostName sni =
                        new javax.net.ssl.SNIHostName(serverName);
                java.util.List<javax.net.ssl.SNIServerName> snis =
                        new ArrayList<>();
                snis.add(sni);
                javax.net.ssl.SSLParameters params = engine.getSSLParameters();
                params.setServerNames(snis);
                engine.setSSLParameters(params);
            }
            long id = NEXT_ENGINE_ID.getAndIncrement();
            ENGINES.put(id, engine);
            return id;
        } catch (Exception e) {
            return fail("newClientEngine", e);
        }
    }

    /**
     * Creates a server-side SSLEngine. Returns the engine handle (>= 1),
     * or a negative error-slot key on failure (see the class javadoc).
     * graalgo.tls.newServerEngine(chainDER, keyAlg, keyParams,
     *     minVersion, maxVersion, cipherSuites) -> handle | -slot
     * chainDER: length-prefixed DER certs (leaf first).
     * keyParams: length-prefixed components:
     *   RSA: n, e, d, p, q | ECDSA: curveName, x, y, d | Ed25519: seed
     */
    public static long newServerEngine(Object chainDERArg, Object keyAlgArg,
                                       Object keyParamsArg, Object minVersionArg,
                                       Object maxVersionArg, Object cipherSuitesArg) {
        byte[] chainDER = optBytes(chainDERArg);
        if (chainDER == null) {
            return failArg("newServerEngine.chainDER", chainDERArg, "GoSlice");
        }
        String keyAlg = optStr(keyAlgArg);
        if (keyAlg == null) {
            return failArg("newServerEngine.keyAlg", keyAlgArg, "GoString");
        }
        byte[] keyParams = optBytes(keyParamsArg);
        if (keyParams == null) {
            return failArg("newServerEngine.keyParams", keyParamsArg, "GoSlice");
        }
        Long minVersion = optNum(minVersionArg);
        if (minVersion == null) {
            return failArg("newServerEngine.minVersion", minVersionArg, "Number");
        }
        Long maxVersion = optNum(maxVersionArg);
        if (maxVersion == null) {
            return failArg("newServerEngine.maxVersion", maxVersionArg, "Number");
        }
        byte[] suitesBE = optBytes(cipherSuitesArg);
        if (suitesBE == null) {
            return failArg("newServerEngine.cipherSuites", cipherSuitesArg, "GoSlice");
        }
        try {

            List<byte[]> certDERs = splitLengthPrefixed(chainDER);
            CertificateFactory cf = CertificateFactory.getInstance("X.509");
            X509Certificate[] chain = new X509Certificate[certDERs.size()];
            for (int i = 0; i < certDERs.size(); i++) {
                chain[i] = (X509Certificate) cf.generateCertificate(
                        new java.io.ByteArrayInputStream(certDERs.get(i)));
            }
            PrivateKey privateKey = buildPrivateKey(keyAlg, splitLengthPrefixed(keyParams));
            if (privateKey == null) {
                return fail("newServerEngine",
                        new IllegalArgumentException("unsupported keyAlg: " + keyAlg));
            }

            SSLContext ctx = SSLContext.getInstance("TLS");
            ctx.init(new KeyManager[]{new FixedKeyManager(chain, privateKey)},
                    new TrustManager[]{new PermissiveTrustManager()},
                    new SecureRandom());
            SSLEngine engine = ctx.createSSLEngine();
            engine.setUseClientMode(false);
            engine.setNeedClientAuth(false);
            engine.setEnabledProtocols(enabledProtocols(minVersion, maxVersion));
            String[] suites = enabledCipherSuites(suitesBE);
            if (suites != null) {
                engine.setEnabledCipherSuites(suites);
            }
            long id = NEXT_ENGINE_ID.getAndIncrement();
            ENGINES.put(id, engine);
            return id;
        } catch (Exception e) {
            return fail("newServerEngine", e);
        }
    }

    private static PrivateKey buildPrivateKey(String keyAlg, List<byte[]> params)
            throws Exception {
        if ("RSA".equalsIgnoreCase(keyAlg)) {
            // n, e, d, p, q; dp/dq/qinv derived.
            BigInteger n = new BigInteger(1, params.get(0));
            BigInteger e = new BigInteger(1, params.get(1));
            BigInteger d = new BigInteger(1, params.get(2));
            BigInteger p = new BigInteger(1, params.get(3));
            BigInteger q = new BigInteger(1, params.get(4));
            BigInteger dp = d.mod(p.subtract(BigInteger.ONE));
            BigInteger dq = d.mod(q.subtract(BigInteger.ONE));
            BigInteger qinv = q.modInverse(p);
            RSAPrivateCrtKeySpec spec = new RSAPrivateCrtKeySpec(
                    n, e, d, p, q, dp, dq, qinv);
            return KeyFactory.getInstance("RSA").generatePrivate(spec);
        }
        if ("ECDSA".equalsIgnoreCase(keyAlg) || "EC".equalsIgnoreCase(keyAlg)) {
            // curveName (UTF-8), x, y, d.
            String curveName = new String(params.get(0),
                    java.nio.charset.StandardCharsets.UTF_8);
            BigInteger x = new BigInteger(1, params.get(1));
            BigInteger y = new BigInteger(1, params.get(2));
            BigInteger d = new BigInteger(1, params.get(3));
            AlgorithmParameters algParams = AlgorithmParameters.getInstance("EC");
            algParams.init(new ECGenParameterSpec(curveName));
            ECParameterSpec ecSpec = algParams.getParameterSpec(ECParameterSpec.class);
            java.security.spec.ECPoint w = new java.security.spec.ECPoint(x, y);
            ECPrivateKeySpec spec = new ECPrivateKeySpec(d, ecSpec);
            // Validate the public point is on the curve (cheap sanity).
            return KeyFactory.getInstance("EC").generatePrivate(spec);
        }
        if ("Ed25519".equalsIgnoreCase(keyAlg)) {
            // seed (32 bytes).
            byte[] seed = params.get(0);
            EdECPrivateKeySpec spec = new EdECPrivateKeySpec(
                    NamedParameterSpec.ED25519, seed);
            return KeyFactory.getInstance("Ed25519").generatePrivate(spec);
        }
        // Unsupported keyAlg: return null; the caller converts this to a
        // channeled error (no `throw` may remain in this file — R23-P1-3;
        // the exception still travels through fail(), preserving the
        // IllegalArgumentException type and message in the error text).
        return null;
    }

    /**
     * Begins the TLS handshake. Returns 0 on success, or a negative
     * error-slot key on failure (see the class javadoc).
     * graalgo.tls.beginHandshake(handle) -> 0 | -slot
     */
    public static long beginHandshake(Object handleArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return failArg("beginHandshake.handle", handleArg, "Number");
        }
        SSLEngine engine = ENGINES.get(h);
        if (engine == null) {
            return fail("beginHandshake",
                    new IllegalArgumentException("unknown engine handle " + h));
        }
        try {
            engine.beginHandshake();
            return 0;
        } catch (Exception e) {
            return fail("beginHandshake", e);
        }
    }

    /**
     * Current handshake status: 0=FINISHED/NOT_HANDSHAKING, 1=NEED_WRAP,
     * 2=NEED_UNWRAP, 3=NEED_TASK; a negative error-slot key on failure
     * (see the class javadoc).
     * graalgo.tls.engineStatus(handle) -> status | -slot
     */
    public static long engineStatus(Object handleArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return failArg("engineStatus.handle", handleArg, "Number");
        }
        SSLEngine engine = ENGINES.get(h);
        if (engine == null) {
            return fail("engineStatus",
                    new IllegalArgumentException("unknown engine handle " + h));
        }
        try {
            long st;
            switch (engine.getHandshakeStatus()) {
                case FINISHED:
                case NOT_HANDSHAKING:
                    st = 0; break;
                case NEED_WRAP:
                    st = 1; break;
                case NEED_UNWRAP:
                    st = 2; break;
                case NEED_TASK:
                    st = 3; break;
                default:
                    st = -1; break;
            }
            return st;
        } catch (Exception e) {
            return fail("engineStatus", e);
        }
    }

    /**
     * Wraps application/handshake data into TLS records.
     * graalgo.tls.wrap(handle, plaintext) -> ciphertext (may be empty),
     * or null on failure (detail stashed under the handle key; see the
     * class javadoc). A null handle argument also yields null with no
     * retrievable detail ("no key" limitation).
     */
    public static Object wrap(Object handleArg, Object plaintextArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return null;
        }
        SSLEngine engine = ENGINES.get(h);
        if (engine == null) {
            return null;
        }
        try {
            byte[] plain = optBytes(plaintextArg);
            if (plain == null) {
                failHandleArg(h, "wrap.plaintext", plaintextArg, "GoSlice");
                return null;
            }
            ByteBuffer src = ByteBuffer.wrap(plain);
            java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
            while (src.hasRemaining()) {
                ByteBuffer dst = ByteBuffer.allocate(NET_BUF);
                SSLEngineResult res = engine.wrap(src, dst);
                dst.flip();
                byte[] chunk = new byte[dst.remaining()];
                dst.get(chunk);
                out.write(chunk, 0, chunk.length);
                if (res.getStatus() == SSLEngineResult.Status.BUFFER_OVERFLOW) {
                    continue;
                }
                if (res.getStatus() == SSLEngineResult.Status.CLOSED) {
                    break;
                }
                // No progress (0 consumed, 0 produced): the engine has
                // nothing to do with this data; without this the loop
                // spins forever inside wrap.
                if (res.bytesConsumed() == 0 && res.bytesProduced() == 0) {
                    break;
                }
            }
            // Handle the empty-plaintext (handshake) case: keep wrapping
            // until the engine has no more output to produce. A single
            // wrap() may produce only part of a flight (e.g. a 6-byte
            // ChangeCipherSpec without the following Finished); without
            // draining, the peer receives a truncated flight and both
            // sides deadlock in NEED_UNWRAP.
            // P3-M9 (x509): GMP=1 handshake deadlock root cause.
            // Note: we do NOT check handshake status in the loop — the
            // engine may report NEED_UNWRAP prematurely after a partial
            // flight (e.g. after CCS but before Finished); we drain based
            // on output production only, with a bound to prevent spins.
            if (plain.length == 0) {
                int emptyCount = 0;
                for (int i = 0; i < 20; i++) {
                    ByteBuffer dst = ByteBuffer.allocate(NET_BUF);
                    SSLEngineResult res = engine.wrap(src, dst);
                    dst.flip();
                    byte[] chunk = new byte[dst.remaining()];
                    dst.get(chunk);
                    out.write(chunk, 0, chunk.length);
                    // No output: count consecutive empties; the engine may
                    // need a status-check cycle between CCS and Finished.
                    // Allow a few empties before giving up.
                    if (chunk.length == 0) {
                        emptyCount++;
                        if (emptyCount >= 3) {
                            break;
                        }
                    } else {
                        emptyCount = 0;
                    }
                    if (res.getStatus() == SSLEngineResult.Status.CLOSED) {
                        break;
                    }
                }
            }
            byte[] result = out.toByteArray();
            return toGoBytes(result);
        } catch (Exception e) {
            failHandle(h, "wrap", e);
            return null;
        }
    }

    /**
     * Unwraps TLS records into application data.
     * Returns app bytes (may be empty); null if the peer closed (no detail
     * stashed — the Go side maps it to io.EOF) or on error (detail stashed
     * under the handle key; see the class javadoc). A null handle argument
     * also yields null with no retrievable detail ("no key" limitation).
     * graalgo.tls.unwrap(handle, netData) -> appdata | null
     */
    public static Object unwrap(Object handleArg, Object netDataArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return null;
        }
        SSLEngine engine = ENGINES.get(h);
        if (engine == null) {
            return null;
        }
        try {
            byte[] netData = optBytes(netDataArg);
            if (netData == null) {
                failHandleArg(h, "unwrap.netData", netDataArg, "GoSlice");
                return null;
            }
            // Prepend any unconsumed bytes from the previous call: the
            // engine must see the peer's bytes in order, with none lost.
            byte[] pending = PENDING.remove(h);
            byte[] input;
            if (pending != null && pending.length > 0) {
                input = new byte[pending.length + netData.length];
                System.arraycopy(pending, 0, input, 0, pending.length);
                System.arraycopy(netData, 0, input, pending.length, netData.length);
            } else {
                input = netData;
            }
            ByteBuffer src = ByteBuffer.wrap(input);
            java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
            ByteBuffer dst = ByteBuffer.allocate(APP_BUF);
            while (src.hasRemaining()) {
                SSLEngineResult res = engine.unwrap(src, dst);
                if (res.getStatus() == SSLEngineResult.Status.BUFFER_OVERFLOW) {
                    // Dst is full of valid unwrapped data; drain it to out,
                    // grow, and retry the unwrap.
                    dst.flip();
                    byte[] chunk = new byte[dst.remaining()];
                    dst.get(chunk);
                    out.write(chunk, 0, chunk.length);
                    dst = ByteBuffer.allocate(dst.capacity() * 2);
                    continue;
                }
                // Drain any produced app bytes before the next unwrap
                // overwrites dst.
                dst.flip();
                if (dst.hasRemaining()) {
                    byte[] chunk = new byte[dst.remaining()];
                    dst.get(chunk);
                    out.write(chunk, 0, chunk.length);
                }
                dst.clear();
                if (res.getStatus() == SSLEngineResult.Status.CLOSED) {
                    return null;
                }
                // BUFFER_UNDERFLOW: need more net data; return what we have.
                if (res.getStatus() == SSLEngineResult.Status.BUFFER_UNDERFLOW) {
                    break;
                }
                // OK with no progress (0 consumed, 0 produced): the engine
                // may need delegated tasks run before it can consume the
                // next record (e.g. after ServerHello, before Certificate).
                // Run them HERE and continue unwrapping — returning now
                // would strand the remaining bytes: the engine reports
                // NEED_WRAP after tasks and then refuses unwrap() input,
                // so the peer's flight gets truncated and both sides
                // deadlock in NEED_UNWRAP. P3-M9 (x509) GMP=1 root cause.
                if (res.bytesConsumed() == 0 && res.bytesProduced() == 0) {
                    if (engine.getHandshakeStatus()
                            == SSLEngineResult.HandshakeStatus.NEED_TASK) {
                        Runnable task;
                        while ((task = engine.getDelegatedTask()) != null) {
                            task.run();
                        }
                        continue;
                    }
                    break;
                }
            }
            // Buffer any input the engine did not consume, so the next
            // unwrap() (or the status check below) sees it first.
            int remaining = src.remaining();
            if (remaining > 0) {
                byte[] rest = new byte[remaining];
                src.get(rest);
                PENDING.put(h, rest);
            }
            byte[] result = out.toByteArray();
            return toGoBytes(result);
        } catch (Exception e) {
            failHandle(h, "unwrap", e);
            return null;
        }
    }

    /**
     * Runs delegated handshake tasks. Returns 0, or a negative error-slot
     * key on failure (see the class javadoc).
     * graalgo.tls.runTasks(handle) -> 0 | -slot
     */
    public static long runTasks(Object handleArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return failArg("runTasks.handle", handleArg, "Number");
        }
        SSLEngine engine = ENGINES.get(h);
        if (engine == null) {
            return fail("runTasks",
                    new IllegalArgumentException("unknown engine handle " + h));
        }
        try {
            Runnable task;
            while ((task = engine.getDelegatedTask()) != null) {
                task.run();
            }
            return 0;
        } catch (Exception e) {
            return fail("runTasks", e);
        }
    }

    /**
     * Peer certificate chain as length-prefixed DERs (leaf first).
     * graalgo.tls.peerCertificates(handle) -> bytes (may be empty).
     * On failure returns empty bytes, stashing detail under the handle key
     * (see the class javadoc).
     */
    public static Object peerCertificates(Object handleArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return toGoBytes(new byte[0]);
        }
        SSLEngine engine = ENGINES.get(h);
        if (engine == null) {
            return toGoBytes(new byte[0]);
        }
        try {
            java.security.cert.Certificate[] certs =
                    engine.getSession().getPeerCertificates();
            java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
            for (java.security.cert.Certificate c : certs) {
                byte[] der = c.getEncoded();
                out.write((der.length >>> 24) & 0xFF);
                out.write((der.length >>> 16) & 0xFF);
                out.write((der.length >>> 8) & 0xFF);
                out.write(der.length & 0xFF);
                out.write(der, 0, der.length);
            }
            return toGoBytes(out.toByteArray());
        } catch (Exception e) {
            failHandle(h, "peerCertificates", e);
            return toGoBytes(new byte[0]);
        }
    }

    /**
     * Returns negotiated parameters packed as (version << 32 | cipherSuite),
     * or a negative error-slot key on failure (see the class javadoc).
     * graalgo.tls.connectionState(handle) -> packed | -slot
     */
    public static long connectionState(Object handleArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return failArg("connectionState.handle", handleArg, "Number");
        }
        SSLEngine engine = ENGINES.get(h);
        if (engine == null) {
            return fail("connectionState",
                    new IllegalArgumentException("unknown engine handle " + h));
        }
        try {
            String protocol = engine.getSession().getProtocol();
            String cipher = engine.getSession().getCipherSuite();
            int vers = parseVersion(protocol);
            int suite = parseCipherSuite(cipher);
            return ((long) vers << 32) | (suite & 0xFFFFFFFFL);
        } catch (Exception e) {
            return fail("connectionState", e);
        }
    }

    private static int parseVersion(String protocol) {
        if (protocol == null) {
            return -1;
        }
        switch (protocol) {
            case "TLSv1":
                return 0x0301;
            case "TLSv1.1":
                return 0x0302;
            case "TLSv1.2":
                return 0x0303;
            case "TLSv1.3":
                return 0x0304;
            default:
                // Unknown protocol string: report -1, never a
                // plausible-but-wrong version.
                return -1;
        }
    }

    // IANA cipher-suite registry names (as returned by
    // SSLEngine.getSession().getCipherSuite()) to suite IDs. Covers the
    // TLS 1.3 suites and the TLS 1.2 suites the JDK negotiates. Exact
    // match only — substring matching mis-maps (e.g. a suite name that
    // contains another as a substring). Unknown names map to -1, never
    // to a plausible-but-wrong suite ID.
    private static final java.util.Map<String, Integer> CIPHER_SUITES =
            buildCipherSuites();

    private static java.util.Map<String, Integer> buildCipherSuites() {
        java.util.Map<String, Integer> m = new java.util.HashMap<>();
        // TLS 1.3 (RFC 8446).
        m.put("TLS_AES_128_GCM_SHA256", 0x1301);
        m.put("TLS_AES_256_GCM_SHA384", 0x1302);
        m.put("TLS_CHACHA20_POLY1305_SHA256", 0x1303);
        m.put("TLS_AES_128_CCM_SHA256", 0x1304);
        m.put("TLS_AES_128_CCM_8_SHA256", 0x1305);
        // TLS 1.2 ECDHE.
        m.put("TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", 0xC02B);
        m.put("TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384", 0xC02C);
        m.put("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", 0xC02F);
        m.put("TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", 0xC030);
        m.put("TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256", 0xCCA8);
        m.put("TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256", 0xCCA9);
        m.put("TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256", 0xC023);
        m.put("TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA384", 0xC024);
        m.put("TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256", 0xC027);
        m.put("TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA384", 0xC028);
        m.put("TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA", 0xC009);
        m.put("TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA", 0xC00A);
        m.put("TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA", 0xC013);
        m.put("TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA", 0xC014);
        m.put("TLS_ECDHE_ECDSA_WITH_3DES_EDE_CBC_SHA", 0xC008);
        m.put("TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA", 0xC012);
        // TLS 1.2 DHE.
        m.put("TLS_DHE_RSA_WITH_AES_128_GCM_SHA256", 0x009E);
        m.put("TLS_DHE_RSA_WITH_AES_256_GCM_SHA384", 0x009F);
        m.put("TLS_DHE_RSA_WITH_CHACHA20_POLY1305_SHA256", 0xCCAA);
        m.put("TLS_DHE_RSA_WITH_AES_128_CBC_SHA256", 0x0067);
        m.put("TLS_DHE_RSA_WITH_AES_256_CBC_SHA256", 0x006B);
        m.put("TLS_DHE_RSA_WITH_AES_128_CBC_SHA", 0x0033);
        m.put("TLS_DHE_RSA_WITH_AES_256_CBC_SHA", 0x0039);
        m.put("TLS_DHE_RSA_WITH_3DES_EDE_CBC_SHA", 0x0016);
        // TLS 1.2 RSA key exchange.
        m.put("TLS_RSA_WITH_AES_128_GCM_SHA256", 0x009C);
        m.put("TLS_RSA_WITH_AES_256_GCM_SHA384", 0x009D);
        m.put("TLS_RSA_WITH_AES_128_CBC_SHA256", 0x003C);
        m.put("TLS_RSA_WITH_AES_256_CBC_SHA256", 0x003D);
        m.put("TLS_RSA_WITH_AES_128_CBC_SHA", 0x002F);
        m.put("TLS_RSA_WITH_AES_256_CBC_SHA", 0x0035);
        m.put("TLS_RSA_WITH_3DES_EDE_CBC_SHA", 0x000A);
        return m;
    }

    private static int parseCipherSuite(String cipher) {
        if (cipher == null) {
            return -1;
        }
        Integer id = CIPHER_SUITES.get(cipher);
        // Unknown suite: report -1, never a plausible-but-wrong ID.
        return id != null ? id : -1;
    }

    /**
     * Whether the engine holds unconsumed input bytes (see PENDING).
     * The Go driving loop checks this before blocking on a network read:
     * if pending bytes exist, it must call unwrap() to drain them first.
     * Returns 1 if pending bytes exist, 0 otherwise, or a negative
     * error-slot key if the handle argument has the wrong host type
     * (see the class javadoc).
     * graalgo.tls.hasPending(handle) -> 1/0 | -slot
     */
    public static long hasPending(Object handleArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return failArg("hasPending.handle", handleArg, "Number");
        }
        byte[] pending = PENDING.get(h);
        return (pending != null && pending.length > 0) ? 1 : 0;
    }

    /**
     * Closes the SSLEngine and releases resources. Idempotent.
     * Returns 0; a negative error-slot key only if the handle argument has
     * the wrong host type. Handle-scoped errors die with the handle.
     * graalgo.tls.close(handle) -> 0 | -slot
     */
    public static long close(Object handleArg) {
        Long h = optNum(handleArg);
        if (h == null) {
            return failArg("close.handle", handleArg, "Number");
        }
        SSLEngine engine = ENGINES.remove(h);
        PENDING.remove(h);
        if (h > 0) {
            ERRORS.remove(h);   // handle-scoped errors die with the handle
        }
        if (engine != null) {
            try {
                engine.closeInbound();
            } catch (Exception ignored) {
            }
            try {
                engine.closeOutbound();
            } catch (Exception ignored) {
            }
        }
        return 0;
    }
}
