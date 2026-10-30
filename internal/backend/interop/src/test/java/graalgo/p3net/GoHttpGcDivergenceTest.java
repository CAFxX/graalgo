package graalgo.p3net;

import com.sun.net.httpserver.HttpServer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.Executors;

/**
 * Acceptance probes for the review-gohttp (P1-6) lane: gc behaviours
 * that current GoHttp does NOT satisfy. These tests FAIL on current
 * code by design, so this class is deliberately NOT registered in
 * {@link graalgo.iface.IfaceTestRunner} — registering it would turn
 * the shared suite red until P1-6 lands.
 *
 * <p>Each probe states the gc behaviour, the current GoHttp behaviour,
 * and what the P1-6 fix must change before the probe can be
 * registered:
 * <ul>
 *   <li>{@link #testRedirectChainWithinGcLimit}: gc's default client
 *       follows up to 10 redirects; Java's NORMAL policy stops at 5.
 *       A 6-hop chain must therefore return 200 under gc and returns
 *       status 0 today. P1-6 must either drive redirects itself or
 *       otherwise honour the gc limit before this probe is registered.
 *   <li>{@link #testErrorKindsDistinguishable}: gc distinguishes
 *       refused-connect, DNS failure, and timeout as different error
 *       kinds; GoHttp collapses every transport failure to the same
 *       24 zero bytes. This probe asserts the two wire encodings
 *       differ; it can only pass once P1-6 emits structured errors.
 *   <li>{@link #testPerRequestTimeoutHonoured}: gc honours
 *       {@code http.Client{Timeout: 2s}}; GoHttp hardcodes 30 s for
 *       both connect and request. clientDo takes no timeout argument
 *       at all, so this probe measures the current 30 s behaviour and
 *       fails on it. It takes ~30 s to run and must never be
 *       registered in the fast suite in this form; after P1-6 adds a
 *       timeout parameter, rewrite it to pass a short timeout against
 *       the blackhole server.
 * </ul>
 *
 * <p>Run standalone (not via the runner) with the /tmp reflection
 * driver used during authoring, or by temporarily registering the
 * class and running {@code make test} in internal/backend/interop.
 */
public final class GoHttpGcDivergenceTest {

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

    private static long statusOf(Object wire) {
        GoSlice gs = (GoSlice) wire;
        byte[] a = (byte[]) gs.array();
        java.nio.ByteBuffer bb = java.nio.ByteBuffer.wrap(a, gs.offset(), gs.len());
        bb.order(java.nio.ByteOrder.BIG_ENDIAN);
        return bb.getLong();
    }

    private static GoSlice headers() {
        return new GoSlice(new Object[0], 0, 0, 0);
    }

    private static GoString gs(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    private static String wireHex(Object wire) {
        GoSlice g = (GoSlice) wire;
        byte[] a = (byte[]) g.array();
        StringBuilder sb = new StringBuilder();
        for (int i = g.offset(); i < g.offset() + g.len(); i++) {
            sb.append(String.format("%02x", a[i]));
        }
        return sb.toString();
    }

    private static int closedPort() throws Exception {
        java.net.ServerSocket ss = new java.net.ServerSocket(0);
        int port = ss.getLocalPort();
        ss.close();
        return port;
    }

    public void testRedirectChainWithinGcLimit() throws Exception {
        HttpServer s = HttpServer.create(new InetSocketAddress("::1", 0), 0);
        for (int i = 1; i <= 6; i++) {
            final String next = (i == 6) ? "/hello" : "/r" + (i + 1);
            s.createContext("/r" + i, ex -> {
                ex.getResponseHeaders().add("Location", next);
                ex.sendResponseHeaders(302, -1);
                ex.close();
            });
        }
        s.createContext("/hello", ex -> {
            byte[] b = "hello-body".getBytes(StandardCharsets.UTF_8);
            ex.sendResponseHeaders(200, b.length);
            try (OutputStream os = ex.getResponseBody()) {
                os.write(b);
            }
        });
        s.setExecutor(Executors.newCachedThreadPool(r -> {
            Thread t = new Thread(r);
            t.setDaemon(true);
            return t;
        }));
        s.start();
        try {
            String base = "http://[::1]:" + s.getAddress().getPort();
            long status = statusOf(GoHttp.clientDo(gs("GET"), gs(base + "/r1"),
                    headers(), null));
            assertEq(200, status, "gc default follows 6 redirects; Java NORMAL stops at 5");
        } finally {
            s.stop(0);
        }
    }

    public void testErrorKindsDistinguishable() throws Exception {
        // gc: refused-connect and DNS failure are different errors.
        // GoHttp today: both collapse to identical 24 zero bytes.
        String refused = wireHex(GoHttp.clientDo(gs("GET"),
                gs("http://[::1]:" + closedPort() + "/"), headers(), null));
        String dns = wireHex(GoHttp.clientDo(gs("GET"),
                gs("http://nonexistent.invalid/"), headers(), null));
        // Guard first: if the sandbox egress proxy answers the DNS
        // failure with an HTTP response instead of failing, the probe
        // is inconclusive here (it must still fail, not pass
        // spuriously on the proxy's status code).
        assertTrue(dns.startsWith("0000000000000000"),
                "DNS failure must surface as a transport error, not an HTTP "
                + "response (proxy interference? wire=" + dns + ")");
        assertTrue(!refused.equals(dns),
                "error kinds must be distinguishable; both are: " + refused);
    }

    public void testPerRequestTimeoutHonoured() throws Exception {
        // Blackhole server: accepts the request and never responds.
        // gc with Client{Timeout: 2s} fails in ~2 s. GoHttp has no
        // timeout parameter and waits its hardcoded 30 s request
        // timeout. This probe asserts the request fails fast (<10 s);
        // it fails today after ~30 s. Slow by design: never register
        // in the fast suite in this form (see class javadoc).
        HttpServer s = HttpServer.create(new InetSocketAddress("::1", 0), 0);
        s.createContext("/blackhole", ex -> {
            try {
                Thread.sleep(35_000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            ex.close();
        });
        s.setExecutor(Executors.newCachedThreadPool(r -> {
            Thread t = new Thread(r);
            t.setDaemon(true);
            return t;
        }));
        s.start();
        try {
            String url = "http://[::1]:" + s.getAddress().getPort() + "/blackhole";
            long t0 = System.nanoTime();
            Object wire = GoHttp.clientDo(gs("GET"), gs(url), headers(), null);
            long dtMs = (System.nanoTime() - t0) / 1_000_000L;
            assertTrue(statusOf(wire) != 200, "blackhole must not succeed");
            assertTrue(dtMs < 10_000L,
                    "per-request timeout must fail fast; took " + dtMs + "ms (hardcoded 30s)");
        } finally {
            s.stop(0);
        }
    }
}
