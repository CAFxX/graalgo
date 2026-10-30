package graalgo.p3net;

import com.sun.net.httpserver.HttpServer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Unit tests for the Java-delegated HTTP client ({@link GoHttp}) against
 * a loopback {@link HttpServer}. Every assertion here is a gc behaviour
 * that current GoHttp satisfies; divergences that current GoHttp does
 * NOT satisfy (per-request timeout, redirect policy, structured error
 * kinds, header order, the 10-redirect default limit) are NOT locked
 * in here — they live as executable probes in
 * {@link GoHttpGcDivergenceTest} for the review-gohttp (P1-6) lane,
 * which owns the clientDo wire change that must accompany them.
 *
 * <p>Loopback transport note: the test server binds IPv6 loopback
 * ({@code ::1}), not 127.0.0.1. The agent sandbox intercepts IPv4
 * loopback TCP (a client "connect" succeeds but the server never
 * accepts, and java.net.http receives a proxy denial as the status
 * line); IPv6 loopback was verified end-to-end with a raw NIO
 * ping/pong before this choice. The server is still loopback-only.
 *
 * <p>One deliberate exception, documented so the P1-6 lane can find it:
 * {@link #testTransportErrorIsNotSuccess} and
 * {@link #testInvalidUrlIsNotSuccess} assert only the coarse,
 * format-independent property that a transport failure is never
 * reported as a success. They parse the current status-0 wire shape to
 * reach that property; when P1-6 replaces status-0 with structured
 * errors, these two tests MUST be rewritten to assert on the new
 * error channel (and the error-kind probes in
 * {@link GoHttpGcDivergenceTest} registered in their place).
 */
public final class GoHttpTest {

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

    private static void assertEq(String want, String got, String msg) {
        if (!want.equals(got)) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    /** Parsed clientDo wire response. */
    private static final class Resp {
        long status;
        Map<String, List<String>> headers = new HashMap<>();
        byte[] body = new byte[0];
    }

    private static Resp parse(Object wire) {
        GoSlice gs = (GoSlice) wire;
        byte[] a = (byte[]) gs.array();
        int p = gs.offset();
        java.nio.ByteBuffer bb = java.nio.ByteBuffer.wrap(a, p, gs.len());
        bb.order(java.nio.ByteOrder.BIG_ENDIAN);
        Resp r = new Resp();
        r.status = bb.getLong();
        long hc = bb.getLong();
        for (long i = 0; i < hc; i++) {
            int kl = (int) bb.getLong();
            byte[] kb = new byte[kl];
            bb.get(kb);
            int vl = (int) bb.getLong();
            byte[] vb = new byte[vl];
            bb.get(vb);
            String k = new String(kb, StandardCharsets.UTF_8).toLowerCase(java.util.Locale.ROOT);
            r.headers.computeIfAbsent(k, x -> new ArrayList<>())
                    .add(new String(vb, StandardCharsets.UTF_8));
        }
        int bl = (int) bb.getLong();
        r.body = new byte[bl];
        bb.get(r.body);
        return r;
    }

    private static GoSlice headers(Object... kv) {
        Object[] arr = new Object[kv.length];
        for (int i = 0; i < kv.length; i++) {
            arr[i] = new GoString(((String) kv[i]).getBytes(StandardCharsets.UTF_8));
        }
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    private static GoSlice body(String s) {
        byte[] b = s.getBytes(StandardCharsets.UTF_8);
        return new GoSlice(b, 0, b.length, b.length);
    }

    private static GoString gs(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    private static int closedPort() throws Exception {
        java.net.ServerSocket ss = new java.net.ServerSocket(0);
        int port = ss.getLocalPort();
        ss.close();
        return port;
    }

    /** Starts a loopback server; caller stops it in finally. */
    private static HttpServer server() throws Exception {
        HttpServer s = HttpServer.create(new InetSocketAddress("::1", 0), 0);
        s.createContext("/hello", ex -> {
            byte[] b = "hello-body".getBytes(StandardCharsets.UTF_8);
            ex.getResponseHeaders().add("X-Test", "hello");
            ex.getResponseHeaders().add("Set-Cookie", "a=1");
            ex.getResponseHeaders().add("Set-Cookie", "b=2");
            ex.sendResponseHeaders(200, b.length);
            try (OutputStream os = ex.getResponseBody()) {
                os.write(b);
            }
        });
        s.createContext("/echo", ex -> {
            byte[] req = ex.getRequestBody().readAllBytes();
            ex.getResponseHeaders().add("X-Echo-Method", ex.getRequestMethod());
            ex.sendResponseHeaders(200, req.length);
            try (OutputStream os = ex.getResponseBody()) {
                os.write(req);
            }
        });
        s.createContext("/redir", ex -> {
            ex.getResponseHeaders().add("Location", "/hello");
            ex.sendResponseHeaders(302, -1);
            ex.close();
        });
        s.createContext("/created", ex -> {
            byte[] b = "created".getBytes(StandardCharsets.UTF_8);
            ex.sendResponseHeaders(201, b.length);
            try (OutputStream os = ex.getResponseBody()) {
                os.write(b);
            }
        });
        s.setExecutor(java.util.concurrent.Executors.newCachedThreadPool(r -> {
            Thread t = new Thread(r);
            t.setDaemon(true);
            return t;
        }));
        s.start();
        return s;
    }

    private static String base(HttpServer s) {
        return "http://[::1]:" + s.getAddress().getPort();
    }

    public void testGetReturnsStatusHeadersBody() throws Exception {
        HttpServer s = server();
        try {
            Resp r = parse(GoHttp.clientDo(gs("GET"), gs(base(s) + "/hello"),
                    headers(), null));
            assertEq(200, r.status, "GET status");
            assertEq("hello-body", new String(r.body, StandardCharsets.UTF_8), "GET body");
            assertTrue(r.headers.getOrDefault("x-test", List.of()).contains("hello"),
                    "X-Test header present: " + r.headers);
        } finally {
            s.stop(0);
        }
    }

    public void testPostEchoRoundTripsBodyAndMethod() throws Exception {
        HttpServer s = server();
        try {
            Resp r = parse(GoHttp.clientDo(gs("POST"), gs(base(s) + "/echo"),
                    headers("Content-Type", "text/plain"), body("post-payload")));
            assertEq(200, r.status, "POST status");
            assertEq("post-payload", new String(r.body, StandardCharsets.UTF_8),
                    "POST echo body");
            assertTrue(r.headers.getOrDefault("x-echo-method", List.of()).contains("POST"),
                    "server saw POST");
        } finally {
            s.stop(0);
        }
    }

    public void testNon200StatusPassesThrough() throws Exception {
        HttpServer s = server();
        try {
            Resp r = parse(GoHttp.clientDo(gs("GET"), gs(base(s) + "/created"),
                    headers(), null));
            assertEq(201, r.status, "201 passes through");
            assertEq("created", new String(r.body, StandardCharsets.UTF_8), "201 body");
        } finally {
            s.stop(0);
        }
    }

    public void testSingleRedirectFollowedLikeGcDefault() throws Exception {
        HttpServer s = server();
        try {
            // gc's default client follows redirects; GoHttp's NORMAL
            // policy agrees on a single hop, so this is shared behaviour,
            // not a divergence lock-in. The gc 10-hop limit (where Java
            // stops at 5) is probed in GoHttpGcDivergenceTest.
            Resp r = parse(GoHttp.clientDo(gs("GET"), gs(base(s) + "/redir"),
                    headers(), null));
            assertEq(200, r.status, "redirect followed to 200");
            assertEq("hello-body", new String(r.body, StandardCharsets.UTF_8),
                    "redirect target body");
        } finally {
            s.stop(0);
        }
    }

    public void testMultiValueHeadersAllPresent() throws Exception {
        HttpServer s = server();
        try {
            Resp r = parse(GoHttp.clientDo(gs("GET"), gs(base(s) + "/hello"),
                    headers(), null));
            // Presence only: GoHttp flattens multi-values via Map
            // iteration, which loses order (P1-6). Order is therefore
            // deliberately NOT asserted here.
            List<String> cookies = r.headers.getOrDefault("set-cookie", List.of());
            assertTrue(cookies.contains("a=1"), "first cookie present: " + cookies);
            assertTrue(cookies.contains("b=2"), "second cookie present: " + cookies);
        } finally {
            s.stop(0);
        }
    }

    public void testRequestHeadersSent() throws Exception {
        HttpServer s = server();
        try {
            s.createContext("/check-header", ex -> {
                String v = ex.getRequestHeaders().getFirst("X-Custom");
                byte[] b = (v == null ? "missing" : v).getBytes(StandardCharsets.UTF_8);
                ex.sendResponseHeaders(200, b.length);
                try (OutputStream os = ex.getResponseBody()) {
                    os.write(b);
                }
            });
            Resp r = parse(GoHttp.clientDo(gs("GET"), gs(base(s) + "/check-header"),
                    headers("X-Custom", "custom-value"), null));
            assertEq(200, r.status, "header check status");
            assertEq("custom-value", new String(r.body, StandardCharsets.UTF_8),
                    "custom header reached the server");
        } finally {
            s.stop(0);
        }
    }

    public void testTransportErrorIsNotSuccess() throws Exception {
        // Coarse contract only (see class javadoc): gc returns a
        // *url.Error here, never a response. Current GoHttp reports
        // status 0; the assertion is "not a success", which must survive
        // the P1-6 structured-error change.
        Resp r = parse(GoHttp.clientDo(gs("GET"),
                gs("http://[::1]:" + closedPort() + "/"),
                headers(), null));
        assertTrue(r.status != 200, "refused connect must not look like success");
        assertEq(0, r.body.length, "error body empty");
    }

    public void testInvalidUrlIsNotSuccess() throws Exception {
        Resp r = parse(GoHttp.clientDo(gs("GET"), gs("ht!tp://bad url"),
                headers(), null));
        assertTrue(r.status != 200, "malformed URL must not look like success");
        assertEq(0, r.body.length, "error body empty");
    }
}
