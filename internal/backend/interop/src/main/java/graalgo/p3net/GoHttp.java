package graalgo.p3net;

import com.oracle.truffle.api.dsl.Cached;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
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
     * @return GoSlice of bytes encoding the response:
     *         int64 BE status, int64 BE headerCount,
     *         per header: int64 BE keyLen, key UTF-8 bytes,
     *                     int64 BE valLen, val UTF-8 bytes,
     *         int64 BE bodyLen, body bytes.
     *         P3-M10: a []byte wire format (not []interface{}) — the linkname
     *         dispatcher returns the target's result unboxed, and Go
     *         .([]interface{}) assertions need descriptor-identical
     *         GoInterface elements which Java cannot mint; a concrete
     *         []byte needs no boxing at all.
     */
    public static Object clientDo(Object method, Object urlStr, Object headers, Object body) {
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
                Object[] harr = (Object[]) hs.array();
                for (int i = 0; i + 1 < hs.len(); i += 2) {
                    String k = toJavaString(harr[hs.offset() + i]);
                    String v = toJavaString(harr[hs.offset() + i + 1]);
                    if (k != null) {
                        rb.header(k, v == null ? "" : v);
                    }
                }
            }
            HttpRequest req = rb.build();
            HttpResponse<byte[]> resp = CLIENT.send(req, HttpResponse.BodyHandlers.ofByteArray());
            int status = resp.statusCode();
            // Collect headers first to count them.
            List<String> hkeys = new ArrayList<>();
            List<String> hvals = new ArrayList<>();
            resp.headers().map().forEach((k, vs) -> {
                for (String v : vs) {
                    hkeys.add(k);
                    hvals.add(v);
                }
            });
            byte[] respBody = resp.body();
            if (respBody == null) {
                respBody = new byte[0];
            }
            java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
            writeI64(out, status);
            writeI64(out, hkeys.size());
            for (int i = 0; i < hkeys.size(); i++) {
                byte[] kb = hkeys.get(i).getBytes(java.nio.charset.StandardCharsets.UTF_8);
                byte[] vb = hvals.get(i).getBytes(java.nio.charset.StandardCharsets.UTF_8);
                writeI64(out, kb.length);
                out.write(kb, 0, kb.length);
                writeI64(out, vb.length);
                out.write(vb, 0, vb.length);
            }
            writeI64(out, respBody.length);
            out.write(respBody, 0, respBody.length);
            byte[] enc = out.toByteArray();
            return goByteSlice(enc);
        } catch (Exception e) {
            // Transport error: status 0, no headers, empty body. The Go
            // side converts status 0 into an error.
            return goByteSlice(new byte[24]);
            // status=0, headerCount=0, bodyLen=0 (all zeros already).
        }
    }

    private static void writeI64(java.io.ByteArrayOutputStream out, long v) {
        for (int i = 7; i >= 0; i--) {
            out.write((int) ((v >>> (i * 8)) & 0xFF));
        }
    }

    /**
     * Converts a Java byte[] to a Go []byte (GoSlice with byte[] backing,
     * D5 canonical form).
     */
    private static GoSlice goByteSlice(byte[] bytes) {
        byte[] arr = bytes.clone();
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    private static String toJavaString(Object o) {
        if (o instanceof GoString) {
            return ((GoString) o).toString();
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
            byte[] out = new byte[s.len()];
            Object arr = s.array();
            if (arr instanceof byte[] barr) {
                System.arraycopy(barr, s.offset(), out, 0, out.length);
                return out;
            }
            // P3-M10: Go []byte is int[]-backed in D5 canonical form.
            if (arr instanceof int[] iarr) {
                for (int i = 0; i < out.length; i++) {
                    out[i] = (byte) iarr[s.offset() + i];
                }
                return out;
            }
            if (arr instanceof Object[] oarr) {
                for (int i = 0; i < out.length; i++) {
                    Object e = oarr[s.offset() + i];
                    out[i] = (byte) (e instanceof Number n ? n.longValue() : 0);
                }
                return out;
            }
            return out;
        }
        if (o instanceof byte[]) {
            return (byte[]) o;
        }
        return null;
    }
}
