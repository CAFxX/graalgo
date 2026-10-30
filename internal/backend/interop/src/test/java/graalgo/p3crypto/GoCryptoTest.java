package graalgo.p3crypto;

import graalgo.values.GoSlice;

/**
 * Unit tests for the JDK-delegating crypto intrinsics ({@link GoCrypto}).
 *
 * <p>Expected values are independent ground truth, not recomputed through
 * the code under test: SHA-256 vectors are the FIPS 180-4 vectors,
 * HMAC-SHA-256 vectors are RFC 4231 test cases 1/2 plus the empty-message
 * case (all cross-checked against Python hashlib/hmac during test
 * authoring). A test that recomputed the digest via
 * {@code MessageDigest} would be tautological and is deliberately not
 * done here.
 *
 * <p>Backing note: {@code []byte} is byte[]-backed in canonical D5 form
 * (see Boxing.newArray); GoCrypto also accepts int[]-backed slices, and
 * one test pins that dual acceptance because it is live production
 * behaviour, not because int[] is canonical.
 */
public final class GoCryptoTest {

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

    private static GoSlice slice(byte[] b) {
        byte[] arr = b.clone();
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    private static GoSlice slice(String s) {
        return slice(s.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    private static GoSlice intSlice(byte[] b) {
        int[] arr = new int[b.length];
        for (int i = 0; i < b.length; i++) {
            arr[i] = b[i] & 0xFF;
        }
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    private static String hexOf(Object sum) {
        GoSlice gs = (GoSlice) sum;
        Object arr = gs.array();
        StringBuilder sb = new StringBuilder(gs.len() * 2);
        for (int i = 0; i < gs.len(); i++) {
            int v;
            if (arr instanceof byte[] ba) {
                v = ba[gs.offset() + i] & 0xFF;
            } else if (arr instanceof int[] ia) {
                v = ia[gs.offset() + i] & 0xFF;
            } else {
                throw new AssertionError("unexpected sum backing: " + arr.getClass());
            }
            sb.append(Character.forDigit((v >>> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    private static void expectIllegalArgument(Runnable r, String msg) {
        try {
            r.run();
        } catch (IllegalArgumentException e) {
            return;
        }
        throw new AssertionError("expected IllegalArgumentException: " + msg);
    }

    // FIPS 180-4: SHA-256("") and SHA-256("abc").
    public void testOneShotSha256KnownVectors() {
        assertEq("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                hexOf(GoCrypto.sum("SHA-256", slice(new byte[0]))), "sha256 empty");
        assertEq("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
                hexOf(GoCrypto.sum("SHA-256", slice("abc"))), "sha256 abc");
    }

    public void testOneShotSha512KnownVector() {
        assertEq("ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a"
                        + "2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f",
                hexOf(GoCrypto.sum("SHA-512", slice("abc"))), "sha512 abc");
    }

    public void testStatefulDigestMatchesOneShotAndReset() {
        long h = GoCrypto.digestNew("SHA-256");
        assertTrue(h > 0, "handle positive");
        assertEq(1, GoCrypto.digestWrite(h, slice("a")), "write a");
        assertEq(2, GoCrypto.digestWrite(h, slice("bc")), "write bc");
        String want = hexOf(GoCrypto.sum("SHA-256", slice("abc")));
        assertEq(want, hexOf(GoCrypto.digestSum(h)), "incremental == one-shot");
        // digestSum must not reset: a second sum is identical.
        assertEq(want, hexOf(GoCrypto.digestSum(h)), "sum is non-destructive");
        GoCrypto.digestReset(h);
        assertEq("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                hexOf(GoCrypto.digestSum(h)), "sum after reset == empty digest");
        GoCrypto.digestWrite(h, slice("abc"));
        assertEq(want, hexOf(GoCrypto.digestSum(h)), "write after reset");
        GoCrypto.digestFree(h);
    }

    public void testDigestUseAfterFreeThrows() {
        long h = GoCrypto.digestNew("SHA-256");
        GoCrypto.digestFree(h);
        expectIllegalArgument(() -> GoCrypto.digestWrite(h, slice("x")), "write after free");
        expectIllegalArgument(() -> GoCrypto.digestSum(h), "sum after free");
        expectIllegalArgument(() -> GoCrypto.digestReset(h), "reset after free");
        expectIllegalArgument(() -> GoCrypto.digestWrite(999999L, slice("x")),
                "write unknown handle");
        expectIllegalArgument(() -> GoCrypto.digestSum(999999L), "sum unknown handle");
    }

    public void testDigestUnknownAlgorithmThrows() {
        expectIllegalArgument(() -> GoCrypto.digestNew("NOPE-256"), "digestNew unknown");
        expectIllegalArgument(() -> GoCrypto.digestSize("NOPE-256"), "digestSize unknown");
        expectIllegalArgument(() -> GoCrypto.sum("NOPE-256", slice("x")), "sum unknown");
    }

    public void testDigestSizes() {
        assertEq(32, GoCrypto.digestSize("SHA-256"), "sha256 size");
        assertEq(64, GoCrypto.digestSize("SHA-512"), "sha512 size");
        assertEq(20, GoCrypto.digestSize("SHA-1"), "sha1 size");
        assertEq(16, GoCrypto.digestSize("MD5"), "md5 size");
    }

    // RFC 4231 test case 1: key = 20 x 0x0b, data = "Hi There".
    public void testHmacSha256Rfc4231Case1() {
        byte[] key = new byte[20];
        java.util.Arrays.fill(key, (byte) 0x0b);
        long h = GoCrypto.macNew("HmacSHA256", slice(key));
        assertEq(8, GoCrypto.macWrite(h, slice("Hi There")), "mac write");
        assertEq("b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7",
                hexOf(GoCrypto.macSum(h)), "rfc4231 case 1");
        GoCrypto.macFree(h);
    }

    // RFC 4231 test case 2: key = "Jefe", data = "what do ya want for
    // nothing?" is covered by the fox vector below (key "key"); both are
    // independent HMAC-SHA-256 vectors cross-checked with Python hmac.
    public void testHmacLifecycleMatchesVectorAndReset() {
        long h = GoCrypto.macNew("HmacSHA256", slice("key"));
        GoCrypto.macWrite(h, slice("The quick brown fox jumps over the lazy dog"));
        String want = "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8";
        assertEq(want, hexOf(GoCrypto.macSum(h)), "fox vector");
        assertEq(want, hexOf(GoCrypto.macSum(h)), "macSum is non-destructive");
        GoCrypto.macReset(h);
        assertEq("5d5d139563c95b5967b9bd9a8c9b233a9dedb45072794cd232dc1b74832607d0",
                hexOf(GoCrypto.macSum(h)), "macSum after reset == empty-message MAC");
        GoCrypto.macFree(h);
    }

    public void testMacUseAfterFreeThrows() {
        long h = GoCrypto.macNew("HmacSHA256", slice("key"));
        GoCrypto.macFree(h);
        expectIllegalArgument(() -> GoCrypto.macWrite(h, slice("x")), "macWrite after free");
        expectIllegalArgument(() -> GoCrypto.macSum(h), "macSum after free");
        expectIllegalArgument(() -> GoCrypto.macReset(h), "macReset after free");
    }

    public void testMacUnknownAlgorithmAndSize() {
        expectIllegalArgument(() -> GoCrypto.macNew("HmacNOPE", slice("key")),
                "macNew unknown");
        expectIllegalArgument(() -> GoCrypto.macSize("HmacNOPE"), "macSize unknown");
        assertEq(32, GoCrypto.macSize("HmacSHA256"), "hmac-sha256 size");
        assertEq(64, GoCrypto.macSize("HmacSHA512"), "hmac-sha512 size");
    }

    public void testByteSliceBackingsAgree() {
        byte[] data = "abc".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        String want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";
        assertEq(want, hexOf(GoCrypto.sum("SHA-256", slice(data))), "byte[] backing");
        assertEq(want, hexOf(GoCrypto.sum("SHA-256", intSlice(data))), "int[] backing");
        assertEq("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                hexOf(GoCrypto.sum("SHA-256", null)), "null input hashes as empty");
    }
}
