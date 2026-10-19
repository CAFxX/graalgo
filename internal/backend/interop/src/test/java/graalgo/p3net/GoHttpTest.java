package graalgo.p3net;

import java.io.IOException;
import java.net.ConnectException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.UnknownHostException;
import java.net.http.HttpConnectTimeoutException;
import java.net.http.HttpTimeoutException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;

import graalgo.values.GoSlice;

/**
 * R-6 red-green: {@link GoHttp#clientDo} must surface transport
 * failures as distinguishable Go errors (gc {@code *url.Error} style),
 * not as one opaque status-0, and must restore the thread's interrupt
 * flag when the failure is an interrupt.
 *
 * <p>Wire format under test (see {@link GoHttp#clientDo}):
 * int64 BE status, int64 BE headerCount, headers, int64 BE bodyLen,
 * body, int64 BE errLen, err UTF-8 bytes.
 */
public final class GoHttpTest {

    // ---------- scaffolding ----------

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

    private static long i64(byte[] b, int off) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v = (v << 8) | (b[off + i] & 0xFFL);
        }
        return v;
    }

    private static GoSlice noHeaders() {
        return new GoSlice(new Object[0], 0, 0, 0);
    }

    /** Decoded wire: status + error string. */
    private static final class Decoded {
        long status;
        String err;
    }

    private static Decoded decode(Object result) {
        assertTrue(result instanceof GoSlice, "clientDo returns a Go []byte");
        GoSlice gs = (GoSlice) result;
        assertTrue(gs.array() instanceof byte[], "byte[]-backed []byte");
        byte[] w = (byte[]) gs.array();
        int p = gs.offset();
        Decoded d = new Decoded();
        d.status = i64(w, p);
        long headerCount = i64(w, p + 8);
        p += 16;
        for (long i = 0; i < headerCount; i++) {
            long kl = i64(w, p);
            p += 8 + (int) kl;
            long vl = i64(w, p);
            p += 8 + (int) vl;
        }
        long bodyLen = i64(w, p);
        p += 8 + (int) bodyLen;
        long errLen = i64(w, p);
        p += 8;
        assertTrue(errLen >= 0 && p + errLen <= w.length,
                "errLen in bounds: " + errLen);
        d.err = new String(w, p, (int) errLen, StandardCharsets.UTF_8);
        return d;
    }

    /** Server socket that accepts and never responds (forces timeouts). */
    private static final class Blackhole implements AutoCloseable {
        final ServerSocket ss;

        Blackhole() throws IOException {
            ss = new ServerSocket(0, 50, InetAddress.getLoopbackAddress());
            Thread.ofPlatform().daemon(true)
                    .name("gohttp-test-blackhole")
                    .uncaughtExceptionHandler((t, e) -> {
                    })
                    .start(() -> {
                        try {
                            while (!ss.isClosed()) {
                                Socket s = ss.accept();
                                try {
                                    Thread.sleep(30_000);
                                } catch (InterruptedException ie) {
                                    Thread.currentThread().interrupt();
                                }
                                try {
                                    s.close();
                                } catch (IOException ignored) {
                                    // best effort
                                }
                            }
                        } catch (IOException ignored) {
                            // ss closed: expected at teardown
                        }
                    });
        }

        int port() {
            return ss.getLocalPort();
        }

        @Override
        public void close() throws IOException {
            ss.close();
        }
    }

    // ---------- tests ----------

    /**
     * True iff loopback TCP is genuine in this environment. Some
     * sandboxes transparently intercept TCP connects (even a connect
     * to 127.0.0.1:1 "succeeds", with a policy denial on first read),
     * which makes refused/timeout outcomes untestable. A refused port
     * must refuse: anything else means interception, and the live
     * tests below skip loudly instead of asserting on filtered traffic.
     */
    private static boolean loopbackTcpIsReal() {
        try (Socket s = new Socket()) {
            s.connect(new InetSocketAddress("127.0.0.1", 1), 2000);
            return false; // intercepted: nothing listens on port 1
        } catch (ConnectException ce) {
            return true; // genuinely refused: loopback is real
        } catch (Exception e) {
            return false;
        }
    }

    /**
     * Refused connection: status 0 with a distinguishable "refused"
     * error (old code: status 0, no error at all).
     */
    public void testRefusedConnectionDistinguishable() {
        if (!loopbackTcpIsReal()) {
            System.out.println(
                    "SKIP testRefusedConnectionDistinguishable: sandbox intercepts loopback TCP");
            return;
        }
        Decoded d = decode(GoHttp.clientDo("GET", "http://127.0.0.1:1/", noHeaders(), null));
        assertEq(0, d.status, "refused: status");
        assertTrue(d.err.startsWith("Get \"http://127.0.0.1:1/\": "),
                "url.Error format, got: " + d.err);
        assertTrue(d.err.contains("refused"), "refused distinguishable, got: " + d.err);
    }

    /** Request timeout against a blackhole: distinguishable timeout error. */
    public void testTimeoutDistinguishable() throws Exception {
        if (!loopbackTcpIsReal()) {
            System.out.println(
                    "SKIP testTimeoutDistinguishable: sandbox intercepts loopback TCP");
            return;
        }
        Duration prev = GoHttp.requestTimeout;
        GoHttp.requestTimeout = Duration.ofMillis(700);
        try (Blackhole bh = new Blackhole()) {
            long t0 = System.nanoTime();
            Decoded d = decode(GoHttp.clientDo(
                    "GET", "http://127.0.0.1:" + bh.port() + "/", noHeaders(), null));
            long dtMs = (System.nanoTime() - t0) / 1_000_000L;
            assertEq(0, d.status, "timeout: status");
            assertTrue(d.err.contains("timeout"),
                    "timeout distinguishable, got: " + d.err);
            assertTrue(dtMs < 20_000, "failed fast on timeout, dtMs=" + dtMs);
        } finally {
            GoHttp.requestTimeout = prev;
        }
    }

    /**
     * An interrupt during the exchange restores the thread's interrupt
     * flag (old code swallowed it) and reports a cancel error.
     */
    public void testInterruptFlagRestored() throws Exception {
        Duration prev = GoHttp.requestTimeout;
        GoHttp.requestTimeout = Duration.ofSeconds(10);
        try (Blackhole bh = new Blackhole()) {
            Thread.currentThread().interrupt();
            Decoded d;
            try {
                d = decode(GoHttp.clientDo(
                        "GET", "http://127.0.0.1:" + bh.port() + "/", noHeaders(), null));
            } finally {
                // Always clear: no test may leak an interrupt flag.
                assertTrue(Thread.interrupted(), "interrupt flag restored by clientDo");
            }
            assertEq(0, d.status, "interrupted: status");
            assertTrue(d.err.contains("canceled"),
                    "interrupt distinguishable, got: " + d.err);
        } finally {
            GoHttp.requestTimeout = prev;
        }
    }

    /**
     * Pure unit pin of the exception-type mapping (no network): each
     * transport failure kind maps to its own gc-style message.
     */
    public void testTransportErrorMapping() {
        String u = "http://example.invalid/";
        assertContains(GoHttp.transportErrorMessage("GET", u,
                new HttpConnectTimeoutException("timed out")), "i/o timeout");
        assertContains(GoHttp.transportErrorMessage("GET", u,
                new HttpTimeoutException("timed out")), "timeout awaiting response headers");
        assertContains(GoHttp.transportErrorMessage("POST", u,
                new IOException(new ConnectException("Connection refused"))), "refused");
        assertContains(GoHttp.transportErrorMessage("GET", u,
                new IOException(new UnknownHostException("example.invalid"))), "no such host");
        assertContains(GoHttp.transportErrorMessage("GET", u,
                new InterruptedException()), "canceled");
        // Op mapping follows gc's urlErrorOp.
        assertTrue(GoHttp.transportErrorMessage("POST", u, new IOException("x"))
                .startsWith("Post \"" + u + "\":"), "POST op mapping");
        assertTrue(GoHttp.transportErrorMessage("GET", u, new IOException("x"))
                .startsWith("Get \"" + u + "\":"), "GET op mapping");
    }

    private static void assertContains(String haystack, String needle) {
        assertTrue(haystack.contains(needle),
                "expected \"" + needle + "\" in \"" + haystack + "\"");
    }
}
