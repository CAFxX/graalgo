package graalgo.p3net;

import graalgo.values.GoSlice;
import graalgo.values.GoString;
import java.io.InterruptedIOException;
import java.net.ConnectException;
import java.net.SocketTimeoutException;
import java.net.URI;
import java.net.UnknownHostException;
import java.net.http.HttpClient;
import java.net.http.HttpConnectTimeoutException;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import javax.net.ssl.SSLException;

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
     *         <p>Success: int64 BE status, int64 BE headerCount,
     *         per header: int64 BE keyLen, key UTF-8 bytes,
     *                     int64 BE valLen, val UTF-8 bytes,
     *         int64 BE bodyLen, body bytes.
     *         P3-M10: a []byte wire format (not []interface{}) — the linkname
     *         dispatcher returns the target's result unboxed, and Go
     *         .([]interface{}) assertions need descriptor-identical
     *         GoInterface elements which Java cannot mint; a concrete
     *         []byte needs no boxing at all.
     *         <p>Transport error (review R1-5 fix): the old encoding
     *         collapsed every failure to 24 zero bytes (status 0,
     *         headerCount 0, bodyLen 0), destroying the error kind and
     *         message and clearing the thread's interrupt flag on
     *         interruption. The error frame is instead:
     *         int64 BE status (= 0, preserving the legacy "error" signal),
     *         int64 BE headerCount (= -1, a sentinel no success frame
     *         can carry, so an error frame is unambiguous),
     *         int64 BE kindLen, kind UTF-8 bytes (one of
     *         {@code interrupted}, {@code timeout}, {@code connect},
     *         {@code dns}, {@code tls}, {@code invalid}, {@code io},
     *         {@code unknown} — see {@link #classifyError}),
     *         int64 BE messageLen, message UTF-8 bytes (the failure's
     *         message, capped; may name the request URL, as gc's
     *         {@code *url.Error} does). There is no body section.
     *         A reader MUST treat headerCount == -1 as an error frame
     *         and surface kind + message; it must never treat the frame
     *         as an empty response. {@link #decodeError} is the
     *         canonical Java-side parser for this format (used by the
     *         tests); a Go-side reader must follow the same layout.
     *         <p>Interruption: if the failure is (or wraps) an
     *         interruption, the thread's interrupt flag is restored
     *         before returning, because
     *         {@code HttpClient.send} clears it when throwing
     *         {@code InterruptedException}.
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
            return goByteSlice(encodeError(e));
        }
    }

    /**
     * Decoded transport-error frame, or {@code null} when {@code enc} is
     * not an error frame (a success frame, or anything else).
     *
     * @throws IllegalArgumentException if the frame carries the error
     *         sentinel but is truncated or malformed — a corrupt error
     *         frame must fail loud, never decode as an empty error.
     */
    static ErrorInfo decodeError(byte[] enc) {
        if (enc == null || enc.length < 16) {
            return null;
        }
        int p = 0;
        long status = readI64(enc, p);
        p += 8;
        long headerCount = readI64(enc, p);
        p += 8;
        if (status != 0 || headerCount != -1) {
            return null;
        }
        String kind = readString(enc, p);
        p += 8 + utf8Len(enc, p);
        String message = readString(enc, p);
        p += 8 + utf8Len(enc, p);
        if (p != enc.length) {
            throw new IllegalArgumentException(
                    "GoHttp error frame has trailing bytes: " + enc.length + " vs " + p);
        }
        return new ErrorInfo(kind, message);
    }

    /** A decoded error frame: the failure kind and message. */
    record ErrorInfo(String kind, String message) {}

    /**
     * Encodes a transport failure as an error frame (see the
     * {@link #clientDo} javadoc). Restores the thread's interrupt flag
     * when the failure is an interruption.
     */
    static byte[] encodeError(Exception e) {
        if (isInterrupted(e)) {
            Thread.currentThread().interrupt();
        }
        String kind = classifyError(e);
        String message = errorMessage(e);
        byte[] kb = kind.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        byte[] mb = message.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
        writeI64(out, 0);
        writeI64(out, -1);
        writeI64(out, kb.length);
        out.write(kb, 0, kb.length);
        writeI64(out, mb.length);
        out.write(mb, 0, mb.length);
        return out.toByteArray();
    }

    /**
     * Classifies a transport failure from its cause chain, by exception
     * type, never by message text, so it does not depend on JDK wording
     * or locale. Priority is by specificity across the WHOLE chain, not
     * by position in it: {@code HttpClient.send} often wraps the
     * specific failure (e.g. a {@code ConnectException}) in a generic
     * {@code IOException}, so a first-match walk would report the
     * wrapper ("io") and lose the cause. The most specific kind found
     * anywhere in the chain wins, in this order: interrupted, connect
     * timeout, timeout, tls, dns, connect, invalid, io. A connect
     * timeout ({@code HttpConnectTimeoutException}, a subclass of
     * {@code HttpTimeoutException}) is a {@code connect} failure, not a
     * generic {@code timeout}.
     */
    static String classifyError(Throwable e) {
        int best = Integer.MAX_VALUE;
        for (Throwable t = e; t != null; t = t.getCause()) {
            int p = priorityOf(t);
            if (p < best) {
                best = p;
            }
        }
        return switch (best) {
            case 1 -> "interrupted";
            case 2 -> "connect";
            case 3 -> "timeout";
            case 4 -> "tls";
            case 5 -> "dns";
            case 6 -> "connect";
            case 7 -> "invalid";
            case 8 -> "io";
            default -> "unknown";
        };
    }

    /** Specificity rank for {@link #classifyError}; MAX_VALUE = unclassifiable. */
    private static int priorityOf(Throwable t) {
        if (t instanceof InterruptedException || t instanceof InterruptedIOException) {
            return 1;
        }
        if (t instanceof HttpConnectTimeoutException) {
            return 2;
        }
        if (t instanceof HttpTimeoutException || t instanceof SocketTimeoutException) {
            return 3;
        }
        if (t instanceof SSLException) {
            return 4;
        }
        if (t instanceof UnknownHostException) {
            return 5;
        }
        if (t instanceof ConnectException) {
            return 6;
        }
        if (t instanceof IllegalArgumentException) {
            return 7;
        }
        if (t instanceof java.io.IOException) {
            return 8;
        }
        return Integer.MAX_VALUE;
    }

    private static boolean isInterrupted(Throwable e) {
        for (Throwable t = e; t != null; t = t.getCause()) {
            if (t instanceof InterruptedException || t instanceof InterruptedIOException) {
                return true;
            }
        }
        return false;
    }

    /**
     * First non-empty message in the cause chain, else the exception's
     * class name. Capped at 4096 characters so a pathological message
     * cannot bloat the wire frame.
     */
    private static String errorMessage(Throwable e) {
        for (Throwable t = e; t != null; t = t.getCause()) {
            String m = t.getMessage();
            if (m != null && !m.isEmpty()) {
                return m.length() > 4096 ? m.substring(0, 4096) : m;
            }
        }
        return e.getClass().getSimpleName();
    }

    private static long readI64(byte[] enc, int p) {
        if (p + 8 > enc.length) {
            throw new IllegalArgumentException("GoHttp error frame truncated at offset " + p);
        }
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v = (v << 8) | (enc[p + i] & 0xFFL);
        }
        return v;
    }

    private static long utf8Len(byte[] enc, int p) {
        long len = readI64(enc, p);
        if (len < 0 || p + 8 + len > enc.length) {
            throw new IllegalArgumentException("GoHttp error frame has bad length at offset " + p);
        }
        return len;
    }

    private static String readString(byte[] enc, int p) {
        long len = utf8Len(enc, p);
        return new String(enc, p + 8, (int) len, java.nio.charset.StandardCharsets.UTF_8);
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
