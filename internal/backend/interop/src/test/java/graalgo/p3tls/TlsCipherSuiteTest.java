package graalgo.p3tls;

import java.util.HashSet;
import java.util.Map;
import java.util.Set;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Regression tests for R23-P1-1+2: the cipher-suite table is a single
 * bidirectional source of truth (IANA id &lt;-&gt; JDK name).
 *
 * <ul>
 *   <li>The CHACHA suites (0xcca8/0xcca9) map to the {@code _SHA256}-suffixed
 *       names the JDK accepts in {@code setEnabledCipherSuites}.</li>
 *   <li>The forward direction covers the FULL reverse (parse) set and the
 *       two directions are exact inverses — no disagreement, no silent gaps.</li>
 *   <li>Unknown suite ids (and malformed lists) are loud errors, never
 *       silent drops that would mutilate the user's preference order.</li>
 *   <li>An explicit {@code CipherSuites} config containing 0xcca8/0xcca9 no
 *       longer crashes engine creation.</li>
 * </ul>
 */
public final class TlsCipherSuiteTest {

    private static void assertEquals(Object expected, Object actual, String what) {
        if (!expected.equals(actual)) {
            throw new AssertionError(
                    what + ": expected <" + expected + "> but was <" + actual + ">");
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    /** R23-P1-1(a): 0xcca8/0xcca9 map to the JDK-accepted _SHA256 names. */
    public void testChachaSuitesMapToSha256SuffixedNames() {
        assertEquals("TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
                TlsRuntime.cipherSuiteName(0xcca8), "0xcca8 forward name");
        assertEquals("TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
                TlsRuntime.cipherSuiteName(0xcca9), "0xcca9 forward name");
    }

    /**
     * R23-P1-1+2: the forward map covers the FULL reverse (parse) set and
     * the two directions are exact inverses.
     */
    public void testForwardAndReverseMapsAgreeOverFullTable() {
        Set<String> names = new HashSet<>();
        for (Map.Entry<String, Integer> e : TlsRuntime.CIPHER_SUITES.entrySet()) {
            String name = e.getKey();
            int id = e.getValue();
            assertTrue(names.add(name), "duplicate name in table: " + name);
            // Reverse direction: JDK name -> id.
            assertEquals(Integer.valueOf(id),
                    Integer.valueOf(TlsRuntime.parseCipherSuite(name)),
                    "parseCipherSuite(" + name + ")");
            // Forward direction: id -> JDK name (was null for ~25 suites
            // pre-fix, silently dropping them from explicit configs).
            assertEquals(name, TlsRuntime.cipherSuiteName(id),
                    "cipherSuiteName(0x" + Integer.toHexString(id) + ")");
        }
        // The table covers the full known set (TLS 1.3 + every TLS 1.2
        // suite the JDK negotiates), not just the 11 the old forward map
        // knew.
        assertEquals(Integer.valueOf(36),
                Integer.valueOf(TlsRuntime.CIPHER_SUITES.size()),
                "full parse-set size");
    }

    /** R23-P1-2(b): an unknown suite id is an ERROR, never a silent drop. */
    public void testUnknownSuiteIdIsLoudError() {
        try {
            TlsRuntime.enabledCipherSuites(new byte[]{0x12, 0x34});
            throw new AssertionError(
                    "expected IllegalArgumentException for unknown suite id 0x1234");
        } catch (IllegalArgumentException expected) {
            assertTrue(expected.getMessage().contains("0x1234"),
                    "error names the offending id, got: " + expected.getMessage());
        }
        // Unknown id among known ids: still loud — never a partially
        // applied, silently mutilated preference order.
        try {
            TlsRuntime.enabledCipherSuites(new byte[]{0x13, 0x01, 0x12, 0x34});
            throw new AssertionError(
                    "expected IllegalArgumentException for unknown suite id 0x1234 among known ids");
        } catch (IllegalArgumentException expected) {
            assertTrue(expected.getMessage().contains("0x1234"),
                    "error names the offending id, got: " + expected.getMessage());
        }
        // Known ids keep their exact preference order (no mutilation).
        String[] suites = TlsRuntime.enabledCipherSuites(
                new byte[]{(byte) 0xcc, (byte) 0xa8, 0x13, 0x01});
        assertEquals(Integer.valueOf(2), Integer.valueOf(suites.length), "suite count");
        assertEquals("TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256", suites[0], "order[0]");
        assertEquals("TLS_AES_128_GCM_SHA256", suites[1], "order[1]");
    }

    /** A truncated (odd-length) suite list is malformed input: loud, not silent. */
    public void testOddLengthSuiteListIsLoudError() {
        try {
            TlsRuntime.enabledCipherSuites(new byte[]{0x13});
            throw new AssertionError(
                    "expected IllegalArgumentException for odd-length suite list");
        } catch (IllegalArgumentException expected) {
            // Expected: the trailing byte must not be silently ignored.
        }
    }

    /** Unknown JDK suite names still parse to -1 (negotiated-state reporting). */
    public void testUnknownSuiteNameParsesToMinusOne() {
        assertEquals(Integer.valueOf(-1),
                Integer.valueOf(TlsRuntime.parseCipherSuite("TLS_FOO_BAR_BAZ")),
                "unknown name");
        assertEquals(Integer.valueOf(-1),
                Integer.valueOf(TlsRuntime.parseCipherSuite(null)),
                "null name");
    }

    /**
     * R23-P1-1(a): an explicit CipherSuites config containing 0xcca8/0xcca9
     * no longer throws (the JDK rejected the old unsuffixed names with
     * IllegalArgumentException, surfacing as a host crash).
     */
    public void testExplicitChachaCipherSuitesConfigSucceeds() {
        byte[] suites = new byte[]{(byte) 0xcc, (byte) 0xa8, (byte) 0xcc, (byte) 0xa9};
        long handle = TlsRuntime.newClientEngine(
                GoString.fromUtf8("example.com"),
                Long.valueOf(0),
                Long.valueOf(0x0303),
                Long.valueOf(0x0303),
                new GoSlice(suites, 0, suites.length, suites.length));
        assertTrue(handle > 0, "expected engine handle, got " + handle);
        TlsRuntime.close(handle);
    }
}
