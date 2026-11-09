package graalgo.p3net;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

import graalgo.p3os.GoOs;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Tests for the {@code graalgo.net.lookuphost} trap ({@link GoNet#lookuphost},
 * R08-S-05, REVIEW-2026-11-09-08): DNS resolution via
 * {@code java.net.InetAddress}.
 *
 * <p>Loopback-only: localhost / 127.0.0.1 / ::1 — no external host is
 * ever resolved. The sandbox DNS is a wildcard (any name resolves), so
 * the not-found case uses a name the JDK rejects locally ("!!!" —
 * {@code UnknownHostException} without a query). The empty-name case
 * is covered at the Go layer: the JDK resolves "" to loopback, so the
 * Go side rejects "" before calling the trap (gc parity).
 *
 * <p>verifies: R08-S-05 (lookuphost blob contract + loopback resolution)
 */
public final class GoNetLookupTest {

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

    /** Calls the trap and returns the raw blob bytes written. */
    private static byte[] lookup(String host, int bufLen) {
        byte[] buf = new byte[bufLen];
        GoSlice gs = new GoSlice(buf, 0, bufLen, bufLen);
        long r = GoNet.lookuphost(
                new GoString(host.getBytes(StandardCharsets.UTF_8)), gs);
        assertTrue(r > 0, "lookuphost(" + host + ") returned " + r);
        byte[] out = new byte[(int) r - 1];
        System.arraycopy(buf, 0, out, 0, out.length);
        return out;
    }

    private static int i32le(byte[] b, int off) {
        return (b[off] & 0xFF) | ((b[off + 1] & 0xFF) << 8)
                | ((b[off + 2] & 0xFF) << 16) | ((b[off + 3] & 0xFF) << 24);
    }

    /** Decodes a blob into the raw address bytes (4 for IPv4, 16 for IPv6). */
    private static List<byte[]> decodeRaw(byte[] blob) {
        int count = i32le(blob, 0);
        assertTrue(count >= 0, "count must be >= 0, got " + count);
        List<byte[]> out = new ArrayList<>();
        int off = 4;
        for (int i = 0; i < count; i++) {
            int len = i32le(blob, off);
            off += 4;
            assertTrue(len == 4 || len == 16, "addr len must be 4 or 16, got " + len);
            byte[] raw = new byte[len];
            System.arraycopy(blob, off, raw, 0, len);
            off += len;
            out.add(raw);
        }
        assertEq(blob.length, off, "blob must be fully consumed");
        return out;
    }

    private static boolean containsIPv4(List<byte[]> addrs, int a, int b, int c, int d) {
        for (byte[] raw : addrs) {
            if (raw.length == 4 && (raw[0] & 0xFF) == a && (raw[1] & 0xFF) == b
                    && (raw[2] & 0xFF) == c && (raw[3] & 0xFF) == d) {
                return true;
            }
        }
        return false;
    }

    private static boolean containsIPv6Loopback(List<byte[]> addrs) {
        for (byte[] raw : addrs) {
            if (raw.length != 16) {
                continue;
            }
            boolean loopback = raw[15] == 1;
            for (int i = 0; i < 15; i++) {
                loopback &= raw[i] == 0;
            }
            if (loopback) {
                return true;
            }
        }
        return false;
    }

    public void testLookupLocalhost() {
        List<byte[]> addrs = decodeRaw(lookup("localhost", 4096));
        // Order is resolver-dependent; pin the set, not the order.
        assertTrue(containsIPv4(addrs, 127, 0, 0, 1),
                "localhost must resolve to 127.0.0.1, got " + addrs.size() + " addrs");
        assertTrue(containsIPv6Loopback(addrs),
                "localhost must resolve to ::1");
    }

    public void testLookupLiteral() {
        List<byte[]> v4 = decodeRaw(lookup("127.0.0.1", 4096));
        assertEq(1, v4.size(), "127.0.0.1 must resolve to exactly one address");
        assertTrue(containsIPv4(v4, 127, 0, 0, 1), "127.0.0.1 must pass through");
        List<byte[]> v6 = decodeRaw(lookup("::1", 4096));
        assertEq(1, v6.size(), "::1 must resolve to exactly one address");
        assertTrue(containsIPv6Loopback(v6), "::1 must pass through");
    }

    public void testLookupUnknown() {
        // "!!!" is not a domain name: the JDK rejects it locally.
        byte[] blob = lookup("!!!", 4096);
        assertEq(4, blob.length, "not-found blob must be exactly the count field");
        assertEq(-1, i32le(blob, 0), "not-found blob count must be -1");
    }

    public void testLookupSmallBuffer() {
        // localhost needs 4 (count) + 8 + 4 (v4) + 8 + 16 (v6) = 40 bytes.
        byte[] buf = new byte[4];
        GoSlice gs = new GoSlice(buf, 0, 4, 4);
        long r = GoNet.lookuphost(
                new GoString("localhost".getBytes(StandardCharsets.UTF_8)), gs);
        assertEq(-GoNet.ENOMEM, r, "too-small buffer must return -ENOMEM");
    }

    public void testLookupBadArg() {
        byte[] buf = new byte[4096];
        GoSlice gs = new GoSlice(buf, 0, 4096, 4096);
        long r = GoNet.lookuphost(null, gs);
        assertEq(-GoOs.EINVAL, r, "null host must return -EINVAL");
        r = GoNet.lookuphost(Long.valueOf(42), gs);
        assertEq(-GoOs.EINVAL, r, "non-string host must return -EINVAL");
    }
}
