package graalgo.p3tls;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for {@link TlsRuntime} (P1-18 remediation, 2026-10-30):
 * engine-error signalling must be distinct from genuine close / empty
 * chains, and the client engine must read its insecure flag.
 *
 * <p>Conventions: TlsRuntime methods take D5-boxed args (strings as
 * GoString, ints/bools as Long, []byte as GoSlice); a Go nil slice
 * arrives as Java null and vice versa. Each test closes the engines it
 * creates.
 */
public final class TlsRuntimeTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoSlice bytes(byte[] a) {
        return new GoSlice(a, 0, a.length, a.length);
    }

    private static GoSlice emptyBytes() {
        return bytes(new byte[0]);
    }

    /** Client engine for "localhost", D5 bool form (Long 0/1). */
    private static long clientEngine(long insecure) {
        Object h = TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                insecure, 0L, 0L, emptyBytes());
        assertTrue(h instanceof Long, "newClientEngine returns Long");
        long handle = (Long) h;
        assertTrue(handle > 0, "newClientEngine returns a positive handle");
        return handle;
    }

    public void testPeerCertificatesBadHandleIsNull() {
        // Was: an empty chain. An unknown handle is an engine error,
        // never "session has no peer certs".
        Object certs = TlsRuntime.peerCertificates(999_999_999L);
        assertTrue(certs == null, "peerCertificates(bad handle) must be null, got: " + certs);
    }

    public void testPeerCertificatesBeforeHandshakeIsNull() {
        long h = clientEngine(0L);
        try {
            // No handshake yet: getPeerCertificates throws
            // SSLPeerUnverifiedException. That engine failure must surface
            // as null, never as an empty chain.
            Object certs = TlsRuntime.peerCertificates(h);
            assertTrue(certs == null, "peerCertificates(pre-handshake) must be null");
        } finally {
            TlsRuntime.close(h);
        }
    }

    public void testWrapUnwrapBadHandleAreNull() {
        Object w = TlsRuntime.wrap(999_999_999L, emptyBytes());
        assertTrue(w == null, "wrap(bad handle) must be null");
        Object u = TlsRuntime.unwrap(999_999_999L, emptyBytes());
        assertTrue(u == null, "unwrap(bad handle) must be null");
        assertTrue(TlsRuntime.engineStatus(999_999_999L) == -1,
                "engineStatus(bad handle) must be -1");
    }

    public void testWrapEngineFailureReturnsNullAndRemovesEngine() {
        long h = clientEngine(1L);
        try {
            // A non-slice plaintext arg makes bytes() throw inside
            // wrap's try block — the same catch path a genuine engine
            // exception takes. Must return null (error), and the dead
            // engine must be removed so engineStatus reports -1.
            Object w = TlsRuntime.wrap(h, GoString.fromUtf8("not-a-slice"));
            assertTrue(w == null, "wrap(engine failure) must be null");
            assertTrue(TlsRuntime.engineStatus(h) == -1,
                    "engineStatus(failed engine) must be -1");
        } finally {
            TlsRuntime.close(h);
        }
    }

    public void testUnwrapEngineFailureReturnsNullAndRemovesEngine() {
        long h = clientEngine(1L);
        try {
            // Same trigger as the wrap test, on the unwrap path: the
            // engine must be removed, so the Go side can tell this
            // error apart from a genuine peer close (whose engine stays
            // registered and reports a status).
            Object u = TlsRuntime.unwrap(h, GoString.fromUtf8("not-a-slice"));
            assertTrue(u == null, "unwrap(engine failure) must be null");
            assertTrue(TlsRuntime.engineStatus(h) == -1,
                    "engineStatus(failed engine) must be -1");
            assertTrue(TlsRuntime.peerCertificates(h) == null,
                    "peerCertificates(removed engine) must be null");
        } finally {
            TlsRuntime.close(h);
        }
    }

    public void testInsecureArgIsRead() {
        // P1-18: insecureArg was accepted and never read. Both D5 forms
        // (Long 0/1, the form the Go side actually passes) must create
        // an engine; the secure form additionally exercises the default
        // trust-manager lookup (fail closed if no default manager
        // exists, rather than silently going permissive).
        long secure = clientEngine(0L);
        long insecure = clientEngine(1L);
        try {
            assertTrue(secure != insecure, "distinct engines get distinct handles");
            assertTrue(TlsRuntime.engineStatus(secure) != -1,
                    "secure engine must report a status");
            assertTrue(TlsRuntime.engineStatus(insecure) != -1,
                    "insecure engine must report a status");
        } finally {
            TlsRuntime.close(secure);
            TlsRuntime.close(insecure);
        }
        // Defensive Boolean form (not the D5 form, but tolerated).
        long bTrue = (Long) TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                Boolean.TRUE, 0L, 0L, emptyBytes());
        long bFalse = (Long) TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                Boolean.FALSE, 0L, 0L, emptyBytes());
        try {
            assertTrue(bTrue > 0 && bFalse > 0, "Boolean insecureArg form accepted");
        } finally {
            TlsRuntime.close(bTrue);
            TlsRuntime.close(bFalse);
        }
    }
}
