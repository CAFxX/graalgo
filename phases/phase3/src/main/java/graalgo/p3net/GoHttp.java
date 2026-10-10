package graalgo.p3net;

import com.oracle.truffle.api.dsl.Cached;
import graalgo.runtime.GoSlice;
import graalgo.runtime.GoString;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;

/**
 * P3-M9 (tier-5: net/http): Java-delegated HTTP client.
 *
 * <p>Per program-owner directive (2026-10-10): prefer delegating to Java's HTTP
 * facilities where semantics match, rather than reimplementing in Go.
 * The Go synthetic net/http Transport.RoundTrip delegates here via the
 * {@code graalgo.http.client.do} linkname intrinsic.
 *
 * <p>Semantics: java.net.http.HttpClient provides a clean semantic match for
 * Go's http.Client transaction model (request → response with status,
 * headers, body). The Go side retains the http.Request/Response types,
 * header canonicalization, and cookie handling.
 */
public final class GoHttp {

    private GoHttp() {}

    private static final HttpClient CLIENT = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(30))
            .followRedirects(HttpClient.Redirect.NORMAL)
            .build();

    /**
     * Intrinsic: graalgo.http.client.do(method, url, headers, body).
     *
     * @param method HTTP method (e.g. "GET")
     * @param urlStr request URL
     * @param headers flat [k, v, k, v, ...] header pairs (GoSlice of GoString)
     * @param body request body bytes (may be null/empty)
     * @return Object[] { Long statusCode, Object[] headerPairs, Object[] bodyBytes }
     */
    public static Object[] clientDo(Object method, Object urlStr, Object headers, Object body) {
        String m = toJavaString(method);
        String u = toJavaString(urlStr);
        try {
            HttpRequest.Builder rb = HttpRequest.newBuilder()
                    .uri(URI.create(u))
                    .timeout(Duration.ofSeconds(30));
            byte[] bodyBytes = toByteArray(body);
            HttpRequest.BodyPublisher pub;
            if (bodyBytes == null || bodyBytes.length == 0) {
                pub = HttpRequest.BodyPublishers.noBody();
            } else {
                pub = HttpRequest.BodyPublishers.ofByteArray(bodyBytes);
            }
            rb.method(m, pub);
            // Headers: flat pairs
            if (headers instanceof GoSlice) {
                GoSlice hs = (GoSlice) headers;
                for (int i = 0; i + 1 < hs.length(); i += 2) {
                    String k = toJavaString(hs.get(i));
                    String v = toJavaString(hs.get(i + 1));
                    if (k != null) {
                        rb.header(k, v == null ? "" : v);
                    }
                }
            }
            HttpRequest req = rb.build();
            HttpResponse<byte[]> resp = CLIENT.send(req, HttpResponse.BodyHandlers.ofByteArray());
            int status = resp.statusCode();
            List<Object> hp = new ArrayList<>();
            resp.headers().map().forEach((k, vs) -> {
                for (String v : vs) {
                    hp.add(GoString.of(k));
                    hp.add(GoString.of(v));
                }
            });
            byte[] respBody = resp.body();
            Object[] bodyOut = new Object[respBody == null ? 0 : respBody.length];
            if (respBody != null) {
                for (int i = 0; i < respBody.length; i++) {
                    bodyOut[i] = (long) (respBody[i] & 0xFF);
                }
            }
            return new Object[]{(long) status, hp.toArray(), bodyOut};
        } catch (Exception e) {
            // Return a synthetic 0 status with the error in a header; the Go
            // side converts non-2xx/valid statuses appropriately.
            // For transport errors, Go returns an error; we signal via status 0.
            return new Object[]{(long) 0, new Object[0], new Object[0]};
        }
    }

    private static String toJavaString(Object o) {
        if (o instanceof GoString) {
            return ((GoString) o).toJavaString();
        }
        if (o instanceof String) {
            return (String) o;
        }
        return o == null ? null : o.toString();
    }

    private static byte[] toByteArray(Object o) {
        if (o == null) {
            return null;
        }
        if (o instanceof GoSlice) {
            GoSlice s = (GoSlice) o;
            byte[] out = new byte[s.length()];
            for (int i = 0; i < out.length; i++) {
                Object e = s.get(i);
                out[i] = (byte) (e instanceof Long ? (Long) e : 0);
            }
            return out;
        }
        if (o instanceof byte[]) {
            return (byte[]) o;
        }
        return null;
    }
}
