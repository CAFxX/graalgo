package graalgo.p3tls;

import java.nio.charset.StandardCharsets;

import graalgo.values.GoString;

/**
 * Tests for the P1-B4 TLS error channel ({@link TlsRuntime#lastError}):
 * wrap/unwrap used to swallow every engine exception and return null,
 * the same sentinel as a clean peer close. Now the exception is
 * recorded per handle and consumed on read, so a nil result with an
 * empty lastError is a clean close and a nil result with a recorded
 * failure is a reportable error.
 *
 * <p>The failure is forced deterministically without a network: a
 * wrong-typed plaintext argument makes {@code bytes()} throw inside
 * wrap/unwrap's try block, exercising exactly the catch → record →
 * lastError pipeline the Go side relies on. The realistic engine
 * failure (SSLHandshakeException on a version mismatch) is pinned
 * end-to-end by test/regression/tls-error-detail under graalgo.
 *
 * <p>verifies: REVIEW-2026-11-03-16 P1-B4.
 */
public final class TlsRuntimeTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(Object want, Object got, String msg) {
        if (!want.equals(got)) {
            throw new AssertionError(msg + ": want <" + want + "> got <" + got + ">");
        }
    }

    private static String lastErrorText(long handle) {
        Object v = TlsRuntime.lastError(handle);
        assertTrue(v instanceof GoString, "lastError returns GoString, got: " + v);
        return new String(((GoString) v).bytes(), StandardCharsets.UTF_8);
    }

    private static long newClientEngine() {
        return TlsRuntime.newClientEngine(GoString.fromUtf8("localhost"),
                Long.valueOf(0), Long.valueOf(0x0303), Long.valueOf(0x0304), null);
    }

    /**
     * verifies: P1-B4 — lastError is empty for an unknown handle (no
     * failure recorded), so the Go side reads an unknown handle's nil
     * result as a clean close, not as a phantom failure.
     */
    public void testLastErrorEmptyForUnknownHandle() {
        assertEq("", lastErrorText(999999L), "lastError for unknown handle");
    }

    /**
     * verifies: P1-B4 — a wrap failure is recorded with its exception,
     * and the record is consumed on read: the first read carries the
     * failure, the second read is empty, so the failure cannot be
     * misattributed to a later, unrelated nil result.
     */
    public void testWrapFailureRecordedAndConsumed() {
        long handle = newClientEngine();
        try {
            assertEq("", lastErrorText(handle), "lastError before failure");
            Object out = TlsRuntime.wrap(handle, "not-a-slice");
            assertTrue(out == null, "wrap with wrong-typed arg returns null");
            String msg = lastErrorText(handle);
            assertTrue(msg.contains("wrap failed"),
                    "recorded failure names the operation: " + msg);
            assertTrue(msg.contains("IllegalArgumentException"),
                    "recorded failure names the exception: " + msg);
            assertEq("", lastErrorText(handle), "lastError consumed on read");
        } finally {
            TlsRuntime.close(handle);
        }
        assertEq("", lastErrorText(handle), "lastError empty after close");
    }

    /**
     * verifies: P1-B4 — the same record-and-consume contract holds for
     * unwrap (the second named swallowing site).
     */
    public void testUnwrapFailureRecordedAndConsumed() {
        long handle = newClientEngine();
        try {
            Object out = TlsRuntime.unwrap(handle, "not-a-slice");
            assertTrue(out == null, "unwrap with wrong-typed arg returns null");
            String msg = lastErrorText(handle);
            assertTrue(msg.contains("unwrap failed"),
                    "recorded failure names the operation: " + msg);
            assertTrue(msg.contains("IllegalArgumentException"),
                    "recorded failure names the exception: " + msg);
            assertEq("", lastErrorText(handle), "lastError consumed on read");
        } finally {
            TlsRuntime.close(handle);
        }
    }
}
