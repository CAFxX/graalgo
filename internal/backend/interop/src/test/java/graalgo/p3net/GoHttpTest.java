package graalgo.p3net;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Tests for {@link GoHttp} (independent-review 2026-10-30, P1-6): the
 * pre-fix version hardcoded a 30&nbsp;s connect timeout and NORMAL redirect
 * policy and collapsed every transport error to status-0 bytes. Every test
 * below asserts gc behaviour (go1.27.1 {@code net/http/client.go}):
 * total timeouts fire, redirect policy is honoured, error kinds are
 * distinguished, and multi-value headers survive.
 *
 * <p>All servers are loopback-only (127.0.0.1 / 127.0.0.2, ephemeral ports).
 * No test depends on wall-clock timing except the timeout test, whose bound
 * (under 2&nbsp;s for a 300&nbsp;ms deadline against a 3&nbsp;s server) is
 * far from both the deadline and the old 30&nbsp;s behaviour.
 *
 * <p>Environment note: these tests need loopback HTTP to actually work.
 * The agent sandbox's {@code other_tcp=Deny} policy replaces every HTTP
 * exchange — even to a loopback server in the same JVM — with a denial
 * response, so all network tests fail there for environmental reasons
 * (proven with a raw-socket probe: TCP connects fine, but the first HTTP
 * bytes get the denial text back instead of the server's response).
 * Run them under CI or anywhere loopback TCP is allowed; do not weaken
 * the assertions to fit the sandbox.
 */
public final class GoHttpTest {

    // ---- small helpers ----

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
            throw new AssertionError(msg + ": want <" + want + "> got <" + got + ">");
        }
    }

    private static GoSlice strSlice(String... pairs) {
        Object[] arr = new Object[pairs.length];
        for (int i = 0; i < pairs.length; i++) {
            arr[i] = GoString.fromUtf8(pairs[i]);
        }
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    private static GoSlice byteSlice(byte[] b) {
        return new GoSlice(b.clone(), 0, b.length, b.length);
    }

    private static GoSlice options(long totalMs, long connectMs, int policy, long maxReq) {
        byte[] b = new byte[32];
        writeI64(b, 0, totalMs);
        writeI64(b, 8, connectMs);
        writeI64(b, 16, policy);
        writeI64(b, 24, maxReq);
        return byteSlice(b);
    }

    private static GoSlice defaultOptions() {
        return options(0, 0, GoHttp.REDIRECT_DEFAULT, 0);
    }

    private static void writeI64(byte[] b, int off, long v) {
        for (int i = 7; i >= 0; i--) {
            b[off + i] = (byte) (v & 0xFF);
            v >>>= 8;
        }
    }

    /** Decoded response: status, header pairs, body, and the error block. */
    private static final class Decoded {
        long status;
        final List<String[]> headers = new ArrayList<>();
        byte[] body = new byte[0];
        long errorKind = GoHttp.ERR_NONE;
        String errorMsg = "";
    }

    private static Decoded decode(Object result) {
        GoSlice s = (GoSlice) result;
        byte[] b = new byte[s.len()];
        System.arraycopy((byte[]) s.array(), s.offset(), b, 0, b.length);
        Decoded d = new Decoded();
        int p = 0;
        d.status = readI64(b, p); p += 8;
        long hc = readI64(b, p); p += 8;
        for (long i = 0; i < hc; i++) {
            long kl = readI64(b, p); p += 8;
            String k = new String(b, p, (int) kl, StandardCharsets.UTF_8); p += (int) kl;
            long vl = readI64(b, p); p += 8;
            String v = new String(b, p, (int) vl, StandardCharsets.UTF_8); p += (int) vl;
            d.headers.add(new String[]{k, v});
        }
        long bl = readI64(b, p); p += 8;
        d.body = new byte[(int) bl];
        System.arraycopy(b, p, d.body, 0, (int) bl); p += (int) bl;
        if (d.status == 0 && p + 16 <= b.length) {
            d.errorKind = readI64(b, p); p += 8;
            long ml = readI64(b, p); p += 8;
            d.errorMsg = new String(b, p, (int) ml, StandardCharsets.UTF_8);
        }
        return d;
    }

    private static long readI64(byte[] b, int off) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v = (v << 8) | (b[off + i] & 0xFF);
        }
        return v;
    }

    private static List<String> headerAll(Decoded d, String name) {
        List<String> out = new ArrayList<>();
        for (String[] kv : d.headers) {
            if (kv[0].equalsIgnoreCase(name)) {
                out.add(kv[1]);
            }
        }
        return out;
    }

    private static HttpServer server(String host, HttpHandler h) throws IOException {
        HttpServer s = HttpServer.create(
                new InetSocketAddress(InetAddress.getByName(host), 0), 0);
        s.createContext("/", h);
        s.start();
        return s;
    }

    private static String base(HttpServer s) {
        return "http://127.0.0.1:" + s.getAddress().getPort();
    }

    private static void send(HttpExchange ex, int status, String body) throws IOException {
        byte[] b = body.getBytes(StandardCharsets.UTF_8);
        ex.sendResponseHeaders(status, b.length);
        try (OutputStream os = ex.getResponseBody()) {
            os.write(b);
        }
    }

    private static void redirect(HttpExchange ex, String loc) throws IOException {
        ex.getResponseHeaders().set("Location", loc);
        send(ex, 302, "");
    }

    // ---- tests ----

    public void testGetOkAndMultiValueHeaders() throws Exception {
        HttpServer s = server("127.0.0.1", ex -> {
            ex.getResponseHeaders().add("X-Multi", "a");
            ex.getResponseHeaders().add("X-Multi", "b");
            send(ex, 200, "hello");
        });
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s) + "/"), strSlice(), byteSlice(new byte[0]),
                    defaultOptions()));
            assertEq(200, d.status, "status");
            assertEq("hello", new String(d.body, StandardCharsets.UTF_8), "body");
            // gc Header values are a []string in order: both values present,
            // in order, as consecutive pairs for the same key.
            List<String> vals = headerAll(d, "X-Multi");
            assertEq(2, vals.size(), "X-Multi count");
            assertEq("a", vals.get(0), "X-Multi[0]");
            assertEq("b", vals.get(1), "X-Multi[1]");
        } finally {
            s.stop(0);
        }
    }

    public void testPostEcho() throws Exception {
        HttpServer s = server("127.0.0.1", ex -> {
            byte[] req = ex.getRequestBody().readAllBytes();
            send(ex, 200, new String(req, StandardCharsets.UTF_8));
        });
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("POST"),
                    GoString.fromUtf8(base(s) + "/echo"),
                    strSlice("Content-Type", "text/plain"),
                    byteSlice("post-body".getBytes(StandardCharsets.UTF_8)),
                    defaultOptions()));
            assertEq(200, d.status, "status");
            assertEq("post-body", new String(d.body, StandardCharsets.UTF_8), "echo");
        } finally {
            s.stop(0);
        }
    }

    public void testRedirectDefaultFollows() throws Exception {
        HttpServer s = server("127.0.0.1", ex -> {
            if (ex.getRequestURI().getPath().equals("/r1")) {
                redirect(ex, "/r2");
            } else {
                send(ex, 200, "final");
            }
        });
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s) + "/r1"), strSlice(), byteSlice(new byte[0]),
                    defaultOptions()));
            assertEq(200, d.status, "redirect followed");
            assertEq("final", new String(d.body, StandardCharsets.UTF_8), "final body");
        } finally {
            s.stop(0);
        }
    }

    public void testRedirectNeverReturns302() throws Exception {
        HttpServer s = server("127.0.0.1", ex -> redirect(ex, "/r2"));
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s) + "/r1"), strSlice(), byteSlice(new byte[0]),
                    options(0, 0, GoHttp.REDIRECT_NEVER, 0)));
            assertEq(302, d.status, "never-policy returns the redirect response");
            assertEq(GoHttp.ERR_NONE, d.errorKind, "never-policy has no error");
        } finally {
            s.stop(0);
        }
    }

    public void testRedirectPostToGetDropsBodyAndBodyHeaders() throws Exception {
        AtomicReference<String> gotMethod = new AtomicReference<>();
        AtomicReference<String> gotContentType = new AtomicReference<>();
        HttpServer s = server("127.0.0.1", ex -> {
            if (ex.getRequestURI().getPath().equals("/p")) {
                redirect(ex, "/get");
            } else {
                gotMethod.set(ex.getRequestMethod());
                gotContentType.set(ex.getRequestHeaders().getFirst("Content-Type"));
                send(ex, 200, "ok");
            }
        });
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("POST"),
                    GoString.fromUtf8(base(s) + "/p"),
                    strSlice("Content-Type", "text/plain"),
                    byteSlice("payload".getBytes(StandardCharsets.UTF_8)),
                    defaultOptions()));
            assertEq(200, d.status, "status");
            // gc redirectBehavior: 302 POST -> GET, body dropped.
            assertEq("GET", gotMethod.get(), "redirected method");
            assertTrue(gotContentType.get() == null, "Content-Type dropped on POST->GET");
        } finally {
            s.stop(0);
        }
    }

    public void testRedirect307PreservesMethodAndBody() throws Exception {
        HttpServer s = server("127.0.0.1", ex -> {
            if (ex.getRequestURI().getPath().equals("/p")) {
                ex.getResponseHeaders().set("Location", "/echo");
                send(ex, 307, "");
            } else {
                byte[] req = ex.getRequestBody().readAllBytes();
                send(ex, 200, ex.getRequestMethod() + ":"
                        + new String(req, StandardCharsets.UTF_8) + ":"
                        + ex.getRequestHeaders().getFirst("Content-Type"));
            }
        });
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("POST"),
                    GoString.fromUtf8(base(s) + "/p"),
                    strSlice("Content-Type", "text/plain"),
                    byteSlice("payload".getBytes(StandardCharsets.UTF_8)),
                    defaultOptions()));
            assertEq(200, d.status, "status");
            assertEq("POST:payload:text/plain",
                    new String(d.body, StandardCharsets.UTF_8), "307 preserves method+body");
        } finally {
            s.stop(0);
        }
    }

    public void testRedirectLoopStopsAfter10Requests() throws Exception {
        AtomicInteger hits = new AtomicInteger();
        HttpServer s = server("127.0.0.1", ex -> {
            hits.incrementAndGet();
            redirect(ex, "/loop");
        });
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s) + "/loop"), strSlice(), byteSlice(new byte[0]),
                    defaultOptions()));
            assertEq(0, d.status, "loop status");
            assertEq(GoHttp.ERR_REDIRECT, d.errorKind, "loop error kind");
            assertTrue(d.errorMsg.contains("stopped after 10 redirects"),
                    "loop message: " + d.errorMsg);
            // gc-verified (go1.27.1 probe): exactly 10 requests, then the error.
            assertEq(10, hits.get(), "loop request count");
        } finally {
            s.stop(0);
        }
    }

    public void testTotalTimeoutFiresWellBefore30s() throws Exception {
        HttpServer s = server("127.0.0.1", ex -> {
            try {
                Thread.sleep(3000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            send(ex, 200, "too-late");
        });
        try {
            long t0 = System.nanoTime();
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s) + "/slow"), strSlice(), byteSlice(new byte[0]),
                    options(300, 0, GoHttp.REDIRECT_DEFAULT, 0)));
            long elapsedMs = (System.nanoTime() - t0) / 1_000_000;
            assertEq(0, d.status, "timeout status");
            assertEq(GoHttp.ERR_TIMEOUT, d.errorKind, "timeout error kind");
            assertTrue(!d.errorMsg.isEmpty(), "timeout message present");
            // The pre-fix client waited 30 s; a 300 ms total deadline must
            // fire in well under 2 s.
            assertTrue(elapsedMs < 2000, "timeout elapsed ms: " + elapsedMs);
        } finally {
            s.stop(0);
        }
    }

    public void testConnectionRefusedKind() throws Exception {
        int port;
        try (ServerSocket ss = new ServerSocket(0, 0, InetAddress.getByName("127.0.0.1"))) {
            port = ss.getLocalPort();
        } // closed immediately: nothing listens.
        Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                GoString.fromUtf8("http://127.0.0.1:" + port + "/"), strSlice(),
                byteSlice(new byte[0]), defaultOptions()));
        assertEq(0, d.status, "refused status");
        assertEq(GoHttp.ERR_CONNECTION, d.errorKind, "refused error kind");
        assertTrue(!d.errorMsg.isEmpty(), "refused message present");
    }

    public void testDnsFailureKind() throws Exception {
        Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                GoString.fromUtf8("http://nonexistent.invalid/"), strSlice(),
                byteSlice(new byte[0]), defaultOptions()));
        assertEq(0, d.status, "dns status");
        assertEq(GoHttp.ERR_DNS, d.errorKind, "dns error kind: " + d.errorMsg);
    }

    public void testBadOptionsFailLoud() {
        // Unknown redirect policy: a custom CheckRedirect cannot cross
        // the intrinsic, so it must fail here, never silently default.
        try {
            GoHttp.clientDo(GoString.fromUtf8("GET"), GoString.fromUtf8("http://127.0.0.1/"),
                    strSlice(), byteSlice(new byte[0]), options(0, 0, 7, 0));
            throw new AssertionError("unknown redirect policy must fail loud");
        } catch (IllegalArgumentException expected) {
            // expected
        }
        try {
            GoHttp.clientDo(GoString.fromUtf8("GET"), GoString.fromUtf8("http://127.0.0.1/"),
                    strSlice(), byteSlice(new byte[0]), byteSlice(new byte[8]));
            throw new AssertionError("short options must fail loud");
        } catch (IllegalArgumentException expected) {
            // expected
        }
        try {
            GoHttp.clientDo(GoString.fromUtf8("GET"), GoString.fromUtf8("http://127.0.0.1/"),
                    strSlice(), byteSlice(new byte[0]), options(-1, 0, 0, 0));
            throw new AssertionError("negative total timeout must fail loud");
        } catch (IllegalArgumentException expected) {
            // expected
        }
    }

    public void testSensitiveHeadersStrippedCrossHostKeptSameHost() throws Exception {
        AtomicReference<String> crossAuth = new AtomicReference<>();
        AtomicReference<String> crossReferer = new AtomicReference<>();
        AtomicReference<String> sameAuth = new AtomicReference<>();
        HttpServer s2 = server("127.0.0.2", ex -> {
            crossAuth.set(ex.getRequestHeaders().getFirst("Authorization"));
            crossReferer.set(ex.getRequestHeaders().getFirst("Referer"));
            send(ex, 200, "cross");
        });
        HttpServer s1 = server("127.0.0.1", ex -> {
            String path = ex.getRequestURI().getPath();
            if (path.equals("/cross")) {
                redirect(ex, "http://127.0.0.2:" + s2.getAddress().getPort() + "/final");
            } else if (path.equals("/same")) {
                redirect(ex, "/final");
            } else {
                sameAuth.set(ex.getRequestHeaders().getFirst("Authorization"));
                send(ex, 200, "same");
            }
        });
        try {
            // Cross-host (127.0.0.1 -> 127.0.0.2): Authorization stripped.
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s1) + "/cross"),
                    strSlice("Authorization", "Bearer secret"), byteSlice(new byte[0]),
                    defaultOptions()));
            assertEq(200, d.status, "cross status");
            assertTrue(crossAuth.get() == null, "Authorization stripped cross-host");
            assertTrue(crossReferer.get() != null && crossReferer.get().contains("/cross"),
                    "Referer set to previous URL: " + crossReferer.get());
            // Same-host: Authorization kept.
            Decoded d2 = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s1) + "/same"),
                    strSlice("Authorization", "Bearer secret"), byteSlice(new byte[0]),
                    defaultOptions()));
            assertEq(200, d2.status, "same status");
            assertEq("Bearer secret", sameAuth.get(), "Authorization kept same-host");
        } finally {
            s1.stop(0);
            s2.stop(0);
        }
    }

    public void testRedirectBodyDropAndSensitiveStripAreSticky() throws Exception {
        // gc stickiness: POST -> 302 (body dropped, method GET) -> 307 must
        // NOT resurrect the body, and a cross-host hop that strips
        // Authorization must NOT restore it on the hop back home.
        AtomicReference<String> echoMethod = new AtomicReference<>();
        AtomicReference<String> echoBody = new AtomicReference<>();
        AtomicReference<String> finalAuth = new AtomicReference<>();
        HttpServer s1 = server("127.0.0.1", ex -> {
            String path = ex.getRequestURI().getPath();
            if (path.equals("/p")) {
                redirect(ex, "/step2");
            } else if (path.equals("/step2")) {
                ex.getResponseHeaders().set("Location", "/echo");
                send(ex, 307, "");
            } else if (path.equals("/echo")) {
                echoMethod.set(ex.getRequestMethod());
                echoBody.set(new String(ex.getRequestBody().readAllBytes(),
                        StandardCharsets.UTF_8));
                send(ex, 200, "echoed");
            } else {
                finalAuth.set(ex.getRequestHeaders().getFirst("Authorization"));
                send(ex, 200, "final");
            }
        });
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("POST"),
                    GoString.fromUtf8(base(s1) + "/p"),
                    strSlice("Content-Type", "text/plain"),
                    byteSlice("payload".getBytes(StandardCharsets.UTF_8)),
                    defaultOptions()));
            assertEq(200, d.status, "sticky status");
            assertEq("GET", echoMethod.get(), "method stays GET after 302");
            assertEq("", echoBody.get(), "body stays dropped after 302, even on 307");
        } finally {
            s1.stop(0);
        }
        // Cross-host strip, then back home: Authorization must stay gone.
        final int[] s1port = new int[1];
        HttpServer home = server("127.0.0.1", ex -> {
            finalAuth.set(ex.getRequestHeaders().getFirst("Authorization"));
            send(ex, 200, "home");
        });
        s1port[0] = home.getAddress().getPort();
        HttpServer away = server("127.0.0.2", ex -> redirect(ex,
                "http://127.0.0.1:" + s1port[0] + "/final"));
        HttpServer start = server("127.0.0.1", ex -> redirect(ex,
                "http://127.0.0.2:" + away.getAddress().getPort() + "/away"));
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(start) + "/start"),
                    strSlice("Authorization", "Bearer secret"), byteSlice(new byte[0]),
                    defaultOptions()));
            assertEq(200, d.status, "sticky-strip status");
            assertTrue(finalAuth.get() == null,
                    "Authorization stays stripped on the hop back home");
        } finally {
            start.stop(0);
            away.stop(0);
            home.stop(0);
        }
    }

    public void testRedirectWithoutLocationReturnedAsIs() throws Exception {
        HttpServer s = server("127.0.0.1", ex -> send(ex, 302, "no-loc"));
        try {
            Decoded d = decode(GoHttp.clientDo(GoString.fromUtf8("GET"),
                    GoString.fromUtf8(base(s) + "/"), strSlice(), byteSlice(new byte[0]),
                    defaultOptions()));
            assertEq(302, d.status, "3xx without Location is not a redirect (gc #17773)");
            assertEq("no-loc", new String(d.body, StandardCharsets.UTF_8), "body");
        } finally {
            s.stop(0);
        }
    }
}
