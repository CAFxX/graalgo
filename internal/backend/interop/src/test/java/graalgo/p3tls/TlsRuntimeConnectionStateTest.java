package graalgo.p3tls;

/**
 * Unit tests for the {@code graalgo.tls.connectionState} contract
 * (REVIEW-2026-11-03-13 P1-3): the Java side must never hand the Go side
 * a pack that decodes to version/cipher 65535. The internal -1 "unknown"
 * sentinel from the parsers is normalized to 0 by packConnectionState,
 * and a missing engine / error yields a whole pack of 0 — gc's zero
 * values for an unnegotiated connection, not 0xffff.
 *
 * <p>The Go-side half of the contract (negative halves in a stale pack
 * also decode to 0) is covered by the graalgo-driver regression test in
 * test/regression/tls-connstate, which exercises ConnectionState through
 * the real trap; the pack cases here pin the Java side of the same
 * contract at its own layer.
 */
public final class TlsRuntimeConnectionStateTest {
    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void checkEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + ", got " + got);
        }
    }

    public void testBadHandleYieldsZeroPack() {
        // No engine is ever registered under Long.MAX_VALUE (handles are
        // allocated upward from 1), so this is the bad-handle path.
        checkEq(0, TlsRuntime.connectionState(Long.MAX_VALUE), "bad handle pack");
    }

    public void testUnknownProtocolMapsToUnknownSentinel() {
        checkEq(-1, TlsRuntime.parseVersion("TLSv9"), "unknown protocol");
        checkEq(-1, TlsRuntime.parseVersion(null), "null protocol");
        checkEq(-1, TlsRuntime.parseVersion(""), "empty protocol");
    }

    public void testKnownProtocolsMapToTlsVersions() {
        checkEq(0x0301, TlsRuntime.parseVersion("TLSv1"), "TLSv1");
        checkEq(0x0302, TlsRuntime.parseVersion("TLSv1.1"), "TLSv1.1");
        checkEq(0x0303, TlsRuntime.parseVersion("TLSv1.2"), "TLSv1.2");
        checkEq(0x0304, TlsRuntime.parseVersion("TLSv1.3"), "TLSv1.3");
    }

    public void testUnknownCipherSuiteMapsToUnknownSentinel() {
        checkEq(-1, TlsRuntime.parseCipherSuite("TLS_FOO_BAR"), "unknown suite");
        checkEq(-1, TlsRuntime.parseCipherSuite(null), "null suite");
        checkEq(-1, TlsRuntime.parseCipherSuite(""), "empty suite");
    }

    public void testKnownCipherSuitesMapToIds() {
        checkEq(0x1301, TlsRuntime.parseCipherSuite("TLS_AES_128_GCM_SHA256"), "TLS 1.3 AES-GCM");
        checkEq(0xC02F, TlsRuntime.parseCipherSuite("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"), "TLS 1.2 ECDHE-RSA");
    }

    public void testPackMapsUnknownHalvesToZero() {
        // Whole-unknown: the pre-fix pack was -1, which the Go unsigned
        // cast turned into version/cipher 65535.
        checkEq(0, TlsRuntime.packConnectionState(-1, -1), "both unknown");
        // Half-bad packs keep the known half and zero the unknown one.
        checkEq(0x1301L, TlsRuntime.packConnectionState(-1, 0x1301), "unknown version, known suite");
        checkEq(0x0303L << 32, TlsRuntime.packConnectionState(0x0303, -1), "known version, unknown suite");
        // Known pair packs exactly.
        checkEq((0x0304L << 32) | 0x1301L,
                TlsRuntime.packConnectionState(0x0304, 0x1301), "known pair");
        // Fully unknown/zero packs to zero, never to 0xffff halves.
        long packed = TlsRuntime.packConnectionState(-1, -1);
        check((packed >>> 32) != 0xFFFF, "version half must not be 0xffff");
        check((packed & 0xFFFF) != 0xFFFF, "suite half must not be 0xffff");
    }
}
