package graalgo.p3net;

import java.net.ConnectException;
import java.net.UnknownHostException;
import java.net.http.HttpConnectTimeoutException;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import javax.net.ssl.SSLHandshakeException;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Tests for the {@code graalgo.http.client.do} intrinsic
 * ({@link GoHttp}) — review R1-5: transport failures must carry their
 * kind and message in the wire format, not collapse to 24 zero bytes,
 * and an interruption must not clear the thread's interrupt flag.
 *
 * <p>No test here needs a live socket: this suite must also run where
 * loopback TCP is intercepted (sandboxed runs), so the live-server
 * success path is covered by construction (the success branch of
 * {@code clientDo} is untouched by the R1-5 fix — see the diff) plus
 * the success-frame format guard below, and failure kinds are covered
 * through the real {@code clientDo} catch path for failures that occur
 * before any socket use (invalid URL, pre-interrupted send) plus
 * direct classification tests for the socket failure types. A live
 * loopback success/interrupt run belongs in CI, where TCP is available.
 */
public final class GoHttpTest {

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

    private static void assertEq(String e, String a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static GoString gs(String s) {
        return GoString.fromUtf8(s);
    }

    /** Extracts the []byte result of {@code clientDo} as a plain byte[]. */
    private static byte[] wire(Object result) {
        assertTrue(result instanceof GoSlice, "clientDo must return a GoSlice, got " + result);
        GoSlice s = (GoSlice) result;
        assertTrue(s.array() instanceof byte[], "clientDo result must be byte[]-backed");
        byte[] arr = (byte[]) s.array();
        byte[] out = new byte[s.len()];
        System.arraycopy(arr, s.offset(), out, 0, out.length);
        return out;
    }

    private static long readI64(byte[] b, int p) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v = (v << 8) | (b[p + i] & 0xFFL);
        }
        return v;
    }

    /** Parses a success frame; fails if the frame is an error frame. */
    private static Map<String, String> parseSuccess(byte[] enc, long[] statusOut, byte[][] bodyOut) {
        assertTrue(GoHttp.decodeError(enc) == null, "success frame must not decode as an error");
        int p = 0;
        long status = readI64(enc, p);
        p += 8;
        long headerCount = readI64(enc, p);
        p += 8;
        assertTrue(headerCount >= 0, "success headerCount must be >= 0, got " + headerCount);
        Map<String, String> headers = new LinkedHashMap<>();
        for (long i = 0; i < headerCount; i++) {
            long kl = readI64(enc, p);
            p += 8;
            String k = new String(enc, p, (int) kl, StandardCharsets.UTF_8);
            p += (int) kl;
            long vl = readI64(enc, p);
            p += 8;
            String v = new String(enc, p, (int) vl, StandardCharsets.UTF_8);
            p += (int) vl;
            headers.put(k, v);
        }
        long bodyLen = readI64(enc, p);
        p += 8;
        assertEq(enc.length, p + bodyLen, "success frame must end exactly at body end");
        byte[] body = new byte[(int) bodyLen];
        System.arraycopy(enc, p, body, 0, body.length);
        statusOut[0] = status;
        bodyOut[0] = body;
        return headers;
    }

    /**
     * Format guard for the untouched success branch: a hand-built frame
     * in the documented success layout (status 200, one header, body)
     * must parse as a success and must NOT decode as an error. This
     * pins the success/error discrimination (status 0 + headerCount -1)
     * that the error frame relies on.
     */
    public void testSuccessFrameIsNotAnError() {
        java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
        writeI64(out, 200);
        writeI64(out, 1);
        byte[] kb = "X-Test".getBytes(StandardCharsets.UTF_8);
        byte[] vb = "yes".getBytes(StandardCharsets.UTF_8);
        writeI64(out, kb.length);
        out.write(kb, 0, kb.length);
        writeI64(out, vb.length);
        out.write(vb, 0, vb.length);
        byte[] body = "hello".getBytes(StandardCharsets.UTF_8);
        writeI64(out, body.length);
        out.write(body, 0, body.length);
        byte[] enc = out.toByteArray();
        long[] status = new long[1];
        byte[][] bodyOut = new byte[1][];
        Map<String, String> headers = parseSuccess(enc, status, bodyOut);
        assertEq(200, status[0], "success status");
        assertEq("hello", new String(bodyOut[0], StandardCharsets.UTF_8), "success body");
        assertEq("yes", headers.get("X-Test"), "success header X-Test");
    }

    private static void writeI64(java.io.ByteArrayOutputStream out, long v) {
        for (int i = 7; i >= 0; i--) {
            out.write((int) ((v >>> (i * 8)) & 0xFF));
        }
    }

    public void testInvalidUrlEncodesKindAndMessage() {
        // "://" is not a valid URI for HttpClient: URI.create throws
        // IllegalArgumentException inside clientDo's try block.
        Object result = GoHttp.clientDo(gs("GET"), gs("://bad url"), null, null);
        byte[] enc = wire(result);
        assertTrue(enc.length > 24,
                "error frame must be longer than the old 24 zero bytes, got " + enc.length);
        GoHttp.ErrorInfo err = GoHttp.decodeError(enc);
        assertTrue(err != null, "invalid-URL result must decode as an error frame");
        assertEq("invalid", err.kind(), "invalid-URL error kind");
        assertTrue(err.message() != null && !err.message().isEmpty(),
                "invalid-URL error message must be non-empty");
    }

    /**
     * Interruption through the real {@code clientDo} catch path, without
     * any socket use: a thread that interrupts itself before calling
     * {@code clientDo} gets {@code InterruptedException} from
     * {@code HttpClient.send} immediately (verified on the pinned JDK:
     * send throws before connecting, with the flag cleared). The
     * result must then be an {@code interrupted} error frame AND the
     * thread's interrupt flag must be set again — proving
     * {@code encodeError} restores what {@code send} cleared. A
     * mid-flight interrupt against a slow server exercises the same
     * catch path and needs loopback TCP, so it belongs in CI.
     */
    public void testInterruptRestoresFlagAndEncodesKind() throws Exception {
        CountDownLatch started = new CountDownLatch(1);
        AtomicReference<byte[]> encRef = new AtomicReference<>();
        AtomicReference<Boolean> flagRef = new AtomicReference<>();
        Thread worker = new Thread(() -> {
            started.countDown();
            Thread.currentThread().interrupt();
            Object result = GoHttp.clientDo(gs("GET"),
                    gs("http://127.0.0.1:9/slow"), null, null);
            // Read the flag immediately: clientDo must have restored
            // it, and nothing between the call and this read clears it.
            flagRef.set(Thread.currentThread().isInterrupted());
            encRef.set(wire(result));
        });
        worker.start();
        assertTrue(started.await(5, TimeUnit.SECONDS), "worker must start");
        worker.join(15_000);
        assertTrue(!worker.isAlive(), "worker must finish after interrupt");
        byte[] enc = encRef.get();
        assertTrue(enc != null, "interrupted request must still return a wire result");
        GoHttp.ErrorInfo err = GoHttp.decodeError(enc);
        assertTrue(err != null, "interrupted request must decode as an error frame");
        assertEq("interrupted", err.kind(), "interrupted error kind");
        assertTrue(Boolean.TRUE.equals(flagRef.get()),
                "clientDo must restore the thread interrupt flag after interruption");
    }

    public void testClassifyErrorByType() {
        assertEq("timeout", GoHttp.classifyError(
                new java.net.http.HttpTimeoutException("timed out")), "HttpTimeoutException");
        assertEq("connect", GoHttp.classifyError(
                new HttpConnectTimeoutException("connect timed out")), "HttpConnectTimeoutException");
        assertEq("tls", GoHttp.classifyError(
                new SSLHandshakeException("handshake failed")), "SSLHandshakeException");
        assertEq("dns", GoHttp.classifyError(
                new UnknownHostException("no such host")), "UnknownHostException");
        assertEq("connect", GoHttp.classifyError(
                new ConnectException("refused")), "ConnectException");
        assertEq("invalid", GoHttp.classifyError(
                new IllegalArgumentException("bad uri")), "IllegalArgumentException");
        assertEq("interrupted", GoHttp.classifyError(
                new RuntimeException("wrap", new InterruptedException())), "wrapped InterruptedException");
        // The real HttpClient shape: a generic IOException wrapper around
        // the specific failure. Specificity must win over position —
        // a first-match walk would report "io" for all of these.
        assertEq("connect", GoHttp.classifyError(
                new java.io.IOException("wrap", new ConnectException("refused"))),
                "IOException wrapping ConnectException");
        assertEq("dns", GoHttp.classifyError(
                new java.io.IOException("wrap", new UnknownHostException("no such host"))),
                "IOException wrapping UnknownHostException");
        assertEq("timeout", GoHttp.classifyError(
                new java.io.IOException("wrap",
                        new java.net.http.HttpTimeoutException("timed out"))),
                "IOException wrapping HttpTimeoutException");
        assertEq("unknown", GoHttp.classifyError(
                new RuntimeException("mystery")), "unclassifiable exception");
    }

    public void testDecodeErrorRejectsSuccessAndMalformed() {
        // A success-shaped frame (status 200, 0 headers, 0 body) is not an error.
        byte[] success = new byte[24];
        success[7] = (byte) 200;
        assertTrue(GoHttp.decodeError(success) == null, "success frame must not decode as error");
        // Error sentinel with a truncated kind section must fail loud, not decode empty.
        byte[] truncated = new byte[16];
        java.util.Arrays.fill(truncated, 8, 16, (byte) 0xFF); // headerCount = -1 (all bits set)
        boolean threw = false;
        try {
            GoHttp.decodeError(truncated);
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        assertTrue(threw, "truncated error frame must throw IllegalArgumentException");
    }
}
