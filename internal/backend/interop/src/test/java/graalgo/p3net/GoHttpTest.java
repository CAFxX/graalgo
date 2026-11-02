package graalgo.p3net;

import java.lang.reflect.Field;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;

/**
 * Regression tests for the 2026-11-02 independent-review P1:
 * {@link GoHttp} imposed a hardcoded 30 s timeout that Go's default
 * client does not have (a 30 s connect timeout on the shared client
 * and a 30 s per-request timeout), breaking slow and streaming
 * responses (long-poll, SSE) at 30 s where gc waits indefinitely
 * (Go's {@code http.Client.Timeout} defaults to 0 = no timeout).
 *
 * <p>Both tests fail against the pre-fix code (connect timeout 30 s,
 * request timeout 30 s) and pass with no timeout set. They are
 * unit-level by design: a delayed-response end-to-end check cannot run
 * in the agent sandbox, where loopback TCP is intercepted
 * (other_tcp=Deny replaces the exchange with a denial response), so a
 * &gt;30 s delayed-response check belongs in CI / an unfiltered
 * environment, not in this runner.
 */
public final class GoHttpTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(Object want, Object got, String msg) {
        if (!want.equals(got)) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    /** The shared client must carry no hardcoded connect timeout. */
    public void testDefaultClientHasNoConnectTimeout() throws Exception {
        Field f = GoHttp.class.getDeclaredField("CLIENT");
        f.setAccessible(true);
        HttpClient client = (HttpClient) f.get(null);
        assertTrue(client.connectTimeout().isEmpty(),
                "CLIENT must have no connect timeout by default, got " + client.connectTimeout());
    }

    /** Requests built for clientDo must carry no per-request timeout. */
    public void testDefaultRequestHasNoTimeout() {
        HttpRequest req = GoHttp.buildRequest("GET", "http://127.0.0.1:9/x", null, null);
        assertEq("GET", req.method(), "method");
        assertEq("http://127.0.0.1:9/x", req.uri().toString(), "uri");
        assertTrue(req.timeout().isEmpty(),
                "request must have no timeout by default, got " + req.timeout());
    }
}
