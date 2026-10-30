package graalgo.p3net;

import graalgo.values.GoSlice;
import graalgo.values.GoString;
import java.net.ConnectException;
import java.net.URI;
import java.net.UnknownHostException;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import javax.net.ssl.SSLException;

/**
 * P3-M9 (tier-5: net/http): Java-delegated HTTP client.
 *
 * <p>Per program-owner directive (2026-10-10): prefer delegating to Java's HTTP
 * facilities where semantics match, rather than reimplementing in Go.
 * The Go synthetic net/http Transport.RoundTrip used to delegate here via the
 * {@code graalgo.http.client.do} linkname intrinsic; the synthetic package was
 * retired 2026-10-16 (real net/http from GOROOT is used now), so this intrinsic
 * currently has no live Go caller. It is kept, and fixed by the 2026-10-30
 * independent-review P1-6 lane, as the correct delegation point for any future
 * caller — and because a silently-wrong intrinsic left registered is worse
 * than a correct one. A future Go caller MUST pass the options argument
 * documented below; there is no default-policy entry point, so a caller that
 * cannot express its policy fails loudly instead of silently diverging.
 *
 * <h2>Why the pre-P1-6 version was wrong (review 2026-10-30, P1-6)</h2>
 *
 * <p>The old version used one process-wide {@code HttpClient} with a hardcoded
 * 30&nbsp;s connect timeout and {@code NORMAL} redirect policy, applied a second
 * hardcoded 30&nbsp;s per-request timeout, and collapsed every transport error
 * to status-0 bytes with no kind or message. A program setting
 * {@code http.Client{Timeout: 2s}} would have waited 30&nbsp;s; a program
 * forbidding redirects would have followed them; a program distinguishing a
 * timeout from a refused connection from a DNS failure could not.
 *
 * <h2>Intrinsic signature (wire format v2)</h2>
 *
 * <p>{@code graalgo.http.client.do(method, url, headers, body, options)}:
 *
 * <ul>
 *   <li>{@code method}: GoString, e.g. {@code "GET"}.
 *   <li>{@code url}: GoString, the initial request URL.
 *   <li>{@code headers}: GoSlice of GoString, flat {@code [k, v, k, v, ...]}
 *       pairs. Multi-value headers are repeated pairs for the same key, in
 *       order; on redirect hops the <em>initial</em> headers are re-applied
 *       (gc's {@code makeHeadersCopier} copies from the initial request, not
 *       from the previous hop).
 *   <li>{@code body}: GoSlice of bytes (may be null/empty). The bytes are
 *       retained so 307/308 redirects can re-send them, exactly as gc
 *       re-sends via {@code GetBody}.
 *   <li>{@code options}: GoSlice of bytes, four int64 big-endian fields:
 *       <ol>
 *         <li>{@code totalTimeoutMillis}: Go {@code http.Client.Timeout} — a
 *             total deadline across all redirect hops, including body read.
 *             {@code 0} means no timeout (the gc default). Negative values
 *             are rejected (fail loud, never silently treated as "no
 *             timeout").
 *         <li>{@code connectTimeoutMillis}: per-hop TCP/TLS connect timeout.
 *             {@code 0} means the Go Transport default of 30&nbsp;s;
 *             negative means no connect timeout.
 *         <li>{@code redirectPolicy}: {@code 0} = DEFAULT (follow redirects
 *             per gc's {@code redirectBehavior} + {@code defaultCheckRedirect},
 *             see below), {@code 1} = NEVER (gc's
 *             {@code ErrUseLastResponse}: the redirect response is returned
 *             as-is with no error). Any other value is rejected with
 *             {@code IllegalArgumentException}. A custom {@code CheckRedirect}
 *             function <em>cannot</em> cross this intrinsic: a future Go
 *             caller that needs one must implement it above the intrinsic
 *             (driving one hop per call with policy NEVER) or fail loudly
 *             before calling — it must never pass policy {@code 0} while
 *             meaning something else.
 *         <li>{@code maxRequests}: maximum requests sent before the redirect
 *             error fires. {@code 0} means the gc default of 10 (verified
 *             against go1.27.1: a redirect loop performs exactly 10 requests,
 *             then errors {@code "stopped after 10 redirects"}). Positive
 *             values override it (the error message then reads
 *             {@code "stopped after N redirects"}); negative values are
 *             rejected.
 *       </ol>
 *       An absent, short, or malformed options slice is rejected with
 *       {@code IllegalArgumentException} — never silently defaulted.
 * </ul>
 *
 * <h2>Redirect semantics (gc {@code net/http/client.go}, go1.27.1)</h2>
 *
 * <p>Redirects are followed in this class, never by the JDK client (every
 * cached client is built with {@code Redirect.NEVER}), because the JDK's
 * redirect policy is not gc's: it has its own hop limit and header rules.
 * Per hop, following gc:
 *
 * <ul>
 *   <li>301/302/303: method becomes GET unless it was GET or HEAD; the body
 *       is dropped, as are body-related headers (Content-Encoding,
 *       Content-Language, Content-Location, Content-Type).
 *   <li>307/308: method and body are preserved.
 *   <li>Sensitive headers (Authorization, Www-Authenticate, Cookie, Cookie2,
 *       Proxy-Authorization, Proxy-Authenticate) are dropped when the
 *       destination host is not the initial host or one of its subdomains
 *       (gc's {@code shouldCopyHeaderOnRedirect}; subdomain comparison here
 *       is ASCII case-insensitive, without gc's punycode normalization —
 *       the only known deviation, and only for non-ASCII hosts).
 *   <li>Referer is set to the previous request URL (without userinfo) unless
 *       the hop is an https→http downgrade or a Referer was explicitly set.
 *   <li>A 3xx response without a Location header is returned as-is (gc issues
 *       #17773/#49281); an unparseable Location is an OTHER error
 *       ({@code "failed to parse Location header ..."}, as in gc).
 *   <li>Cookie-jar semantics are NOT implemented: there is no jar on this
 *       side of the intrinsic, so Set-Cookie headers are returned to the
 *       caller untouched and no cookies are synthesized on hops beyond the
 *       initial request's own Cookie header. A future Go caller that needs
 *       jar behavior must implement it above the intrinsic.
 * </ul>
 *
 * <h2>Response wire format (v1 layout, extended)</h2>
 *
 * <p>On success the bytes are exactly the v1 layout, so existing
 * success-path decoders keep working:
 *
 * <pre>
 *   int64 BE status, int64 BE headerCount,
 *   per header: int64 BE keyLen, key UTF-8 bytes,
 *               int64 BE valLen, val UTF-8 bytes,
 *   int64 BE bodyLen, body bytes.
 * </pre>
 *
 * <p>Header pairs preserve per-key value order and keep same-key pairs
 * consecutive, so a decoder grouping consecutive equal keys recovers Go's
 * {@code Header} {@code map[string][]string} values exactly. Cross-key
 * order is the JDK map's order, not wire order — that is not a gc-observable
 * difference, because Go's {@code Header} is itself a map.
 *
 * <p>On any transport or policy error the encoding is the old 24 zero bytes
 * (status 0, headerCount 0, bodyLen 0 — so a v1-only decoder still reads
 * "status 0, empty body", exactly as before) <em>followed by</em> an error
 * block:
 *
 * <pre>
 *   int64 BE errorKind, int64 BE messageLen, message UTF-8 bytes.
 * </pre>
 *
 * <p>{@code errorKind}: 0 = none (never present in an error block),
 * 1 = TIMEOUT, 2 = CONNECTION (refused/reset/unreachable), 3 = DNS
 * (unresolvable host), 4 = TLS, 5 = REDIRECT (too many redirects),
 * 6 = OTHER. The message is gc-style {@code Op "url": detail}, where
 * {@code Op} follows gc's {@code urlErrorOp} (first letter upper, rest
 * lower-cased for ASCII methods; empty method reads {@code "Get"}) and
 * {@code url} is the request URL in flight (for redirect errors, the raw
 * Location value, matching gc's {@code ue.URL = loc}). The detail text
 * comes from the JDK exception and is diagnostic only — callers must
 * branch on {@code errorKind}, never parse the message.
 *
 * <p>Known limitation, stated not hidden: {@code java.net.http} rejects
 * certain restricted request headers (e.g. {@code Host},
 * {@code Content-Length}) at build time. Such a header surfaces as an
 * OTHER error, not as a silent drop. A future Go caller must map
 * {@code req.Host} and content length outside the header list.
 */
public final class GoHttp {

    private GoHttp() {}

    /** Error kinds for the trailing error block; see the class javadoc. */
    public static final int ERR_NONE = 0;
    public static final int ERR_TIMEOUT = 1;
    public static final int ERR_CONNECTION = 2;
    public static final int ERR_DNS = 3;
    public static final int ERR_TLS = 4;
    public static final int ERR_REDIRECT = 5;
    public static final int ERR_OTHER = 6;

    /** Redirect policies for options field 2; see the class javadoc. */
    public static final int REDIRECT_DEFAULT = 0;
    public static final int REDIRECT_NEVER = 1;

    /** Go Transport default dial timeout, used when options field 1 is 0. */
    private static final long DEFAULT_CONNECT_TIMEOUT_MS = 30_000;
    /** gc defaultCheckRedirect: at most this many requests, then the error. */
    private static final int DEFAULT_MAX_REQUESTS = 10;

    private static final int[] REDIRECT_STATUSES = {301, 302, 303, 307, 308};

    private static final String[] SENSITIVE_HEADERS = {
        "authorization", "www-authenticate", "cookie", "cookie2",
        "proxy-authorization", "proxy-authenticate"
    };
    private static final String[] BODY_HEADERS = {
        "content-encoding", "content-language", "content-location", "content-type"
    };

    /**
     * Clients are cached by normalized connect timeout only: redirect policy
     * is never a client property (the loop below implements it), and the
     * total timeout is a per-request deadline, so neither belongs in the
     * cache key. Sharing clients preserves the JDK connection pool across
     * calls with the same connect timeout, which a per-request client
     * would destroy (no keep-alive reuse).
     */
    private static final ConcurrentHashMap<Long, HttpClient> CLIENTS = new ConcurrentHashMap<>();

    private static HttpClient clientFor(long connectTimeoutMs) {
        long key = connectTimeoutMs == 0 ? DEFAULT_CONNECT_TIMEOUT_MS : connectTimeoutMs;
        return CLIENTS.computeIfAbsent(key, k -> {
            HttpClient.Builder b = HttpClient.newBuilder()
                    .followRedirects(HttpClient.Redirect.NEVER);
            if (k > 0) {
                b.connectTimeout(Duration.ofMillis(k));
            }
            return b.build();
        });
    }

    /**
     * Intrinsic: graalgo.http.client.do(method, url, headers, body, options).
     * See the class javadoc for the options and response wire formats.
     */
    public static Object clientDo(Object method, Object urlStr, Object headers, Object body,
            Object options) {
        String m = toJavaString(method);
        String u = toJavaString(urlStr);
        if (m == null) {
            m = "";
        }
        if (u == null) {
            u = "";
        }
        Options opts = parseOptions(options);
        List<String[]> initialHeaders = flatHeaders(headers);
        byte[] initialBody = toByteArray(body);
        if (initialBody == null) {
            initialBody = new byte[0];
        }

        URI curUri;
        try {
            curUri = URI.create(u);
        } catch (IllegalArgumentException e) {
            return errorResult(ERR_OTHER, m, u, detailOf(e));
        }

        long deadlineNanos = opts.totalTimeoutMillis > 0
                ? System.nanoTime() + Duration.ofMillis(opts.totalTimeoutMillis).toNanos()
                : 0;
        String curMethod = m;
        List<String[]> curHeaders = initialHeaders;
        byte[] curBody = initialBody;
        URI initialUri = curUri;
        int attempts = 0;
        // gc stickiness (client.go do()): once a hop drops the body, or
        // once sensitive headers are stripped, later hops never restore
        // them — even a hop back to the original host or a 307 after a
        // 302. Both flags are therefore loop-persistent, not per-hop.
        boolean bodyDropped = false;
        boolean sensitiveStripped = false;

        while (true) {
            attempts++;
            Duration remaining = null;
            if (deadlineNanos != 0) {
                long left = deadlineNanos - System.nanoTime();
                if (left <= 0) {
                    return errorResult(ERR_TIMEOUT, m, curUri.toString(),
                            "Client.Timeout exceeded while awaiting headers");
                }
                remaining = Duration.ofNanos(left);
            }
            HttpResponse<byte[]> resp;
            try {
                HttpRequest.Builder rb = HttpRequest.newBuilder()
                        .uri(curUri);
                if (remaining != null) {
                    rb.timeout(remaining);
                }
                HttpRequest.BodyPublisher pub = curBody.length == 0
                        ? HttpRequest.BodyPublishers.noBody()
                        : HttpRequest.BodyPublishers.ofByteArray(curBody);
                rb.method(curMethod, pub);
                for (String[] kv : curHeaders) {
                    rb.header(kv[0], kv[1]);
                }
                resp = clientFor(opts.connectTimeoutMillis)
                        .send(rb.build(), HttpResponse.BodyHandlers.ofByteArray());
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return errorResult(ERR_OTHER, m, curUri.toString(), detailOf(e));
            } catch (Exception e) {
                return errorResult(kindOf(e), m, curUri.toString(), detailOf(e));
            }

            int status = resp.statusCode();
            if (!isRedirectStatus(status)) {
                return successResult(resp);
            }
            String loc = resp.headers().firstValue("Location").orElse("");
            if (loc.isEmpty()) {
                // gc issues #17773/#49281: a 3xx without Location is not a redirect.
                return successResult(resp);
            }
            if (opts.redirectPolicy == REDIRECT_NEVER) {
                return successResult(resp);
            }
            if (attempts >= opts.maxRequests) {
                return errorResult(ERR_REDIRECT, m, loc,
                        "stopped after " + opts.maxRequests + " redirects");
            }
            URI nextUri;
            try {
                nextUri = curUri.resolve(URI.create(loc));
            } catch (IllegalArgumentException e) {
                return errorResult(ERR_OTHER, m, curUri.toString(),
                        "failed to parse Location header \"" + loc + "\": " + detailOf(e));
            }
            boolean includeBody;
            String nextMethod;
            if (status == 307 || status == 308) {
                nextMethod = curMethod;
                includeBody = !bodyDropped;
            } else { // 301, 302, 303: gc redirectBehavior.
                nextMethod = (curMethod.equals("GET") || curMethod.equals("HEAD"))
                        ? curMethod : "GET";
                includeBody = false;
            }
            if (!includeBody) {
                bodyDropped = true;
            }
            if (!sensitiveStripped
                    && !java.util.Objects.equals(initialUri.getHost(), nextUri.getHost())
                    && !isSameOrSubdomain(initialUri.getHost(), nextUri.getHost())) {
                sensitiveStripped = true;
            }
            List<String[]> nextHeaders = new ArrayList<>();
            for (String[] kv : initialHeaders) {
                String lower = kv[0].toLowerCase(java.util.Locale.ROOT);
                if (sensitiveStripped && contains(SENSITIVE_HEADERS, lower)) {
                    continue;
                }
                if (!includeBody && contains(BODY_HEADERS, lower)) {
                    continue;
                }
                nextHeaders.add(kv);
            }
            if (!hasHeader(nextHeaders, "referer")) {
                String ref = refererFor(curUri, nextUri);
                if (!ref.isEmpty()) {
                    nextHeaders.add(new String[]{"Referer", ref});
                }
            }
            curUri = nextUri;
            curMethod = nextMethod;
            curHeaders = nextHeaders;
            curBody = includeBody ? initialBody : new byte[0];
        }
    }

    private static boolean isRedirectStatus(int status) {
        for (int s : REDIRECT_STATUSES) {
            if (s == status) {
                return true;
            }
        }
        return false;
    }

    /** gc shouldCopyHeaderOnRedirect, ASCII-only (no punycode normalization). */
    private static boolean isSameOrSubdomain(String initialHost, String destHost) {
        if (initialHost == null || destHost == null) {
            return false;
        }
        String ih = initialHost.toLowerCase(java.util.Locale.ROOT);
        String dh = destHost.toLowerCase(java.util.Locale.ROOT);
        return dh.equals(ih) || dh.endsWith("." + ih);
    }

    /** gc refererForURL: previous URL sans userinfo, unless https→http. */
    private static String refererFor(URI prev, URI next) {
        if ("https".equalsIgnoreCase(prev.getScheme())
                && "http".equalsIgnoreCase(next.getScheme())) {
            return "";
        }
        try {
            return new URI(prev.getScheme(), null, prev.getHost(), prev.getPort(),
                    prev.getPath(), prev.getQuery(), prev.getFragment()).toString();
        } catch (Exception e) {
            return prev.toString();
        }
    }

    private static boolean contains(String[] set, String lower) {
        for (String s : set) {
            if (s.equals(lower)) {
                return true;
            }
        }
        return false;
    }

    private static boolean hasHeader(List<String[]> headers, String name) {
        for (String[] kv : headers) {
            if (kv[0].equalsIgnoreCase(name)) {
                return true;
            }
        }
        return false;
    }

    private static List<String[]> flatHeaders(Object headers) {
        List<String[]> out = new ArrayList<>();
        if (headers instanceof GoSlice hs) {
            Object[] harr = (Object[]) hs.array();
            for (int i = 0; i + 1 < hs.len(); i += 2) {
                String k = toJavaString(harr[hs.offset() + i]);
                String v = toJavaString(harr[hs.offset() + i + 1]);
                if (k != null) {
                    out.add(new String[]{k, v == null ? "" : v});
                }
            }
        }
        return out;
    }

    private static Object successResult(HttpResponse<byte[]> resp) {
        int status = resp.statusCode();
        List<String> hkeys = new ArrayList<>();
        List<String> hvals = new ArrayList<>();
        // Per-key value order is the JDK list order; same-key pairs stay
        // consecutive, so grouping consecutive equal keys recovers Go's
        // Header map[string][]string values. Cross-key order is the JDK
        // map's order, which Go cannot observe (Header is a map).
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
        return goByteSlice(out.toByteArray());
    }

    /**
     * v1-compatible error encoding: the old 24 zero bytes, then the error
     * block (kind + gc-style message). A v1-only decoder still reads
     * status 0 with an empty body; a v2 decoder reads the trailing block.
     */
    private static Object errorResult(int kind, String method, String url, String detail) {
        java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
        writeI64(out, 0); // status
        writeI64(out, 0); // headerCount
        writeI64(out, 0); // bodyLen
        String msg = urlErrorOp(method) + " \"" + url + "\": " + detail;
        byte[] mb = msg.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        writeI64(out, kind);
        writeI64(out, mb.length);
        out.write(mb, 0, mb.length);
        return goByteSlice(out.toByteArray());
    }

    /** gc urlErrorOp: empty → "Get"; else first char + ASCII-lowered rest. */
    private static String urlErrorOp(String method) {
        if (method == null || method.isEmpty()) {
            return "Get";
        }
        StringBuilder sb = new StringBuilder(method.length());
        sb.append(method.charAt(0));
        for (int i = 1; i < method.length(); i++) {
            char c = method.charAt(i);
            sb.append(c >= 'A' && c <= 'Z' ? (char) (c + 32) : c);
        }
        return sb.toString();
    }

    private static int kindOf(Exception e) {
        if (e instanceof HttpTimeoutException) {
            return ERR_TIMEOUT;
        }
        if (e instanceof UnknownHostException) {
            return ERR_DNS;
        }
        if (e instanceof SSLException) {
            return ERR_TLS;
        }
        if (e instanceof ConnectException) {
            return ERR_CONNECTION;
        }
        // A malformed HTTP response (e.g. an unparseable status line) is
        // a protocol failure, not a connection failure: it proves the
        // connection worked and the peer spoke garbage.
        if (e instanceof java.net.ProtocolException) {
            return ERR_OTHER;
        }
        // HttpConnectTimeoutException extends HttpTimeoutException and is
        // caught above; other IOExceptions (reset, unreachable, closed)
        // are connection failures, not a separate kind.
        if (e instanceof java.io.IOException) {
            return ERR_CONNECTION;
        }
        return ERR_OTHER;
    }

    private static String detailOf(Exception e) {
        String msg = e.getMessage();
        if (msg != null && !msg.isEmpty()) {
            return msg;
        }
        return e.getClass().getSimpleName();
    }

    /** Options wire format: four int64 BE fields; see the class javadoc. */
    private record Options(long totalTimeoutMillis, long connectTimeoutMillis,
            int redirectPolicy, int maxRequests) {}

    private static Options parseOptions(Object options) {
        byte[] raw = toByteArray(options);
        if (raw == null || raw.length < 32) {
            throw new IllegalArgumentException(
                    "graalgo.http.client.do: options must be 4 int64 BE fields "
                            + "(totalTimeoutMillis, connectTimeoutMillis, redirectPolicy, maxRequests)");
        }
        long total = readI64(raw, 0);
        long connect = readI64(raw, 8);
        long policy = readI64(raw, 16);
        long maxReq = readI64(raw, 24);
        if (total < 0) {
            throw new IllegalArgumentException(
                    "graalgo.http.client.do: negative totalTimeoutMillis " + total);
        }
        if (policy != REDIRECT_DEFAULT && policy != REDIRECT_NEVER) {
            throw new IllegalArgumentException(
                    "graalgo.http.client.do: unknown redirectPolicy " + policy
                            + " (0 = default, 1 = never; a custom CheckRedirect cannot cross this intrinsic)");
        }
        if (maxReq < 0) {
            throw new IllegalArgumentException(
                    "graalgo.http.client.do: negative maxRequests " + maxReq);
        }
        return new Options(total, connect, (int) policy,
                maxReq == 0 ? DEFAULT_MAX_REQUESTS : (int) Math.min(maxReq, Integer.MAX_VALUE));
    }

    private static long readI64(byte[] b, int off) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v = (v << 8) | (b[off + i] & 0xFF);
        }
        return v;
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
