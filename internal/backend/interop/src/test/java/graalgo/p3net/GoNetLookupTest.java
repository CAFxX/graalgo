package graalgo.p3net;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

import graalgo.p3os.GoOs;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for {@link GoNet#lookupHost} (REVIEW-2026-11-03-16 S7):
 * the JVM-resolver intrinsic behind synthetic net's LookupHost.
 *
 * <p>Only deterministic cases are asserted: IP literals, localhost
 * (the sandbox hosts file), and the small-buffer path. DNS failure
 * is not asserted here — the sandbox resolver's behaviour for
 * unresolvable names is environment-dependent (gc's own probe
 * returned success for "nonexistent.invalid" here); the sync-run
 * e2e compares graalgo against gc in the same environment instead.
 */
public final class GoNetLookupTest {
    private static GoSlice byteBuf(int n) {
        return new GoSlice(new int[n], 0, n, n);
    }

    private static List<String> decodeBlob(GoSlice buf, long n) {
        int[] arr = (int[]) buf.array();
        int len = (int) n - 1;
        List<String> out = new ArrayList<>();
        int off = buf.offset();
        int count = i32(arr, off);
        int p = off + 4;
        for (int i = 0; i < count; i++) {
            int alen = i32(arr, p);
            p += 4;
            byte[] b = new byte[alen];
            for (int j = 0; j < alen; j++) {
                b[j] = (byte) arr[p + j];
            }
            p += alen;
            out.add(new String(b, StandardCharsets.UTF_8));
        }
        if (p - off != len) {
            throw new AssertionError("blob length mismatch: used "
                    + (p - off) + ", reported " + len);
        }
        return out;
    }

    private static int i32(int[] arr, int off) {
        return (arr[off] & 0xFF)
                | ((arr[off + 1] & 0xFF) << 8)
                | ((arr[off + 2] & 0xFF) << 16)
                | ((arr[off + 3] & 0xFF) << 24);
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testIpLiteral() {
        GoSlice buf = byteBuf(4096);
        long r = GoNet.lookupHost(GoString.fromUtf8("127.0.0.1"), buf);
        check(r > 1, "IP literal must succeed, got " + r);
        List<String> addrs = decodeBlob(buf, r);
        check(addrs.size() == 1 && "127.0.0.1".equals(addrs.get(0)),
                "IP literal addrs: " + addrs);
    }

    public void testLocalhost() {
        GoSlice buf = byteBuf(4096);
        long r = GoNet.lookupHost(GoString.fromUtf8("localhost"), buf);
        check(r > 1, "localhost must succeed, got " + r);
        List<String> addrs = decodeBlob(buf, r);
        check(addrs.contains("127.0.0.1"),
                "localhost must contain 127.0.0.1: " + addrs);
    }

    public void testSmallBuffer() {
        GoSlice buf = byteBuf(4);
        long r = GoNet.lookupHost(GoString.fromUtf8("127.0.0.1"), buf);
        check(r == -GoNet.ENOMEM, "small buffer must be -ENOMEM, got " + r);
    }

    public void testBadArg() {
        GoSlice buf = byteBuf(4096);
        long r = GoNet.lookupHost(Long.valueOf(1), buf);
        check(r == -GoOs.EINVAL, "non-string host must be -EINVAL, got " + r);
    }
}
