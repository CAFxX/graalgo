package graalgo.p3crypto;

import graalgo.values.GoSlice;

/**
 * Unit tests for {@link GoCrypto} (R-03 remediation, 2026-10-31).
 *
 * <p>Every digest/HMAC expectation below is a published test vector
 * (NIST / RFC 4231), not a value produced by the code under test, so a
 * wrong delegation (wrong algorithm, truncated output, swapped key/data)
 * fails the test. Stateful-handle tests pin the handle contract the Go
 * side relies on: {@code digestSum}/{@code macSum} do not reset, reset
 * restores the initial state, and use-after-free fails loud.
 */
public final class GoCryptoTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static void assertEq(String e, String a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /** Go []byte with byte[] backing (the D5 canonical form GoCrypto returns). */
    private static GoSlice slice(byte[] b) {
        return new GoSlice(b.clone(), 0, b.length, b.length);
    }

    /** Go []byte with int[] backing (the D5 form the Go side may pass in). */
    private static GoSlice intSlice(byte[] b) {
        int[] ia = new int[b.length];
        for (int i = 0; i < b.length; i++) {
            ia[i] = b[i] & 0xFF;
        }
        return new GoSlice(ia, 0, ia.length, ia.length);
    }

    private static byte[] bytesOf(Object o) {
        GoSlice gs = (GoSlice) o;
        Object arr = gs.array();
        byte[] out = new byte[gs.len()];
        if (arr instanceof byte[] ba) {
            System.arraycopy(ba, gs.offset(), out, 0, out.length);
        } else if (arr instanceof int[] ia) {
            for (int i = 0; i < out.length; i++) {
                out[i] = (byte) ia[gs.offset() + i];
            }
        } else {
            throw new AssertionError("unexpected backing " + arr.getClass());
        }
        return out;
    }

    private static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    private static byte[] hexBytes(String h) {
        byte[] out = new byte[h.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(h.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    private static byte[] ascii(String s) {
        return s.getBytes(java.nio.charset.StandardCharsets.US_ASCII);
    }

    public void testSha256AbcVector() {
        assertEq("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
                hex(bytesOf(GoCrypto.sum("SHA-256", slice(ascii("abc"))))),
                "SHA-256(abc) must match the NIST vector");
    }

    public void testSha256EmptyVector() {
        assertEq("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                hex(bytesOf(GoCrypto.sum("SHA-256", slice(new byte[0])))),
                "SHA-256(empty) must match the NIST vector");
    }

    public void testSha512AndSha1AndMd5Vectors() {
        assertEq("ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f",
                hex(bytesOf(GoCrypto.sum("SHA-512", slice(ascii("abc"))))),
                "SHA-512(abc) must match the NIST vector");
        assertEq("a9993e364706816aba3e25717850c26c9cd0d89d",
                hex(bytesOf(GoCrypto.sum("SHA-1", slice(ascii("abc"))))),
                "SHA-1(abc) must match the NIST vector");
        assertEq("900150983cd24fb0d6963f7d28e17f72",
                hex(bytesOf(GoCrypto.sum("MD5", slice(ascii("abc"))))),
                "MD5(abc) must match the RFC 1321 vector");
    }

    public void testIntBackedInputMatchesByteBackedInput() {
        // D5 passes []byte either way; a delegation that only handled one
        // backing would hash different bytes (or throw) for the other.
        String a = hex(bytesOf(GoCrypto.sum("SHA-256", slice(ascii("abc")))));
        String b = hex(bytesOf(GoCrypto.sum("SHA-256", intSlice(ascii("abc")))));
        assertEq(a, b, "int[]-backed and byte[]-backed inputs must hash identically");
    }

    public void testStatefulSplitWritesEqualOneShot() {
        long id = GoCrypto.digestNew("SHA-256");
        try {
            assertEq(1, GoCrypto.digestWrite(id, slice(ascii("a"))), "first write length");
            assertEq(2, GoCrypto.digestWrite(id, slice(ascii("bc"))), "second write length");
            assertEq("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
                    hex(bytesOf(GoCrypto.digestSum(id))),
                    "split writes must equal the one-shot vector");
        } finally {
            GoCrypto.digestFree(id);
        }
    }

    public void testDigestSumDoesNotReset() {
        long id = GoCrypto.digestNew("SHA-256");
        try {
            GoCrypto.digestWrite(id, slice(ascii("abc")));
            String first = hex(bytesOf(GoCrypto.digestSum(id)));
            String second = hex(bytesOf(GoCrypto.digestSum(id)));
            assertEq(first, second, "digestSum must be repeatable (no reset)");
            GoCrypto.digestWrite(id, slice(ascii("d")));
            assertTrue(!hex(bytesOf(GoCrypto.digestSum(id))).equals(first),
                    "write after sum must change the digest");
        } finally {
            GoCrypto.digestFree(id);
        }
    }

    public void testDigestResetRestoresInitialState() {
        long id = GoCrypto.digestNew("SHA-256");
        try {
            GoCrypto.digestWrite(id, slice(ascii("abc")));
            GoCrypto.digestReset(id);
            GoCrypto.digestWrite(id, slice(ascii("abc")));
            assertEq("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
                    hex(bytesOf(GoCrypto.digestSum(id))),
                    "reset then re-write must reproduce the vector");
        } finally {
            GoCrypto.digestFree(id);
        }
    }

    public void testDigestUseAfterFreeFailsLoud() {
        long id = GoCrypto.digestNew("SHA-256");
        GoCrypto.digestFree(id);
        try {
            GoCrypto.digestWrite(id, slice(ascii("x")));
            throw new AssertionError("digestWrite on a freed handle must throw");
        } catch (IllegalArgumentException expected) {
            assertTrue(expected.getMessage().contains("unknown digest handle"),
                    "message must name the bad handle: " + expected.getMessage());
        }
        try {
            GoCrypto.digestSum(id);
            throw new AssertionError("digestSum on a freed handle must throw");
        } catch (IllegalArgumentException expected) {
            // Expected.
        }
    }

    public void testDigestUnknownAlgorithmFailsLoud() {
        try {
            GoCrypto.digestNew("NOPE-256");
            throw new AssertionError("unknown digest algorithm must throw");
        } catch (IllegalArgumentException expected) {
            // Expected.
        }
        try {
            GoCrypto.sum("NOPE-256", slice(new byte[0]));
            throw new AssertionError("unknown one-shot algorithm must throw");
        } catch (IllegalArgumentException expected) {
            // Expected.
        }
    }

    public void testDigestSizes() {
        assertEq(32, GoCrypto.digestSize("SHA-256"), "SHA-256 size");
        assertEq(64, GoCrypto.digestSize("SHA-512"), "SHA-512 size");
        assertEq(20, GoCrypto.digestSize("SHA-1"), "SHA-1 size");
        assertEq(16, GoCrypto.digestSize("MD5"), "MD5 size");
    }

    public void testHmacSha256Rfc4231Case1() {
        // RFC 4231 test case 1: key = 20 x 0x0b, data = "Hi There".
        byte[] key = new byte[20];
        java.util.Arrays.fill(key, (byte) 0x0b);
        long id = GoCrypto.macNew("HmacSHA256", slice(key));
        try {
            GoCrypto.macWrite(id, slice(ascii("Hi There")));
            assertEq("b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7",
                    hex(bytesOf(GoCrypto.macSum(id))),
                    "HMAC-SHA256 RFC 4231 case 1 must match");
        } finally {
            GoCrypto.macFree(id);
        }
    }

    public void testHmacSha256Rfc4231Case2() {
        // RFC 4231 test case 2: key = "Jefe", data = "what do ya want for nothing?".
        long id = GoCrypto.macNew("HmacSHA256", slice(ascii("Jefe")));
        try {
            GoCrypto.macWrite(id, slice(ascii("what do ya want for nothing?")));
            assertEq("5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843",
                    hex(bytesOf(GoCrypto.macSum(id))),
                    "HMAC-SHA256 RFC 4231 case 2 must match");
        } finally {
            GoCrypto.macFree(id);
        }
    }

    public void testMacSumDoesNotResetAndResetRestores() {
        long id = GoCrypto.macNew("HmacSHA256", slice(ascii("key")));
        try {
            GoCrypto.macWrite(id, slice(ascii("data")));
            String first = hex(bytesOf(GoCrypto.macSum(id)));
            assertEq(first, hex(bytesOf(GoCrypto.macSum(id))),
                    "macSum must be repeatable (no reset)");
            GoCrypto.macReset(id);
            GoCrypto.macWrite(id, slice(ascii("data")));
            assertEq(first, hex(bytesOf(GoCrypto.macSum(id))),
                    "reset then re-write must reproduce the MAC");
        } finally {
            GoCrypto.macFree(id);
        }
    }

    public void testMacUseAfterFreeAndUnknownAlgorithmFailLoud() {
        long id = GoCrypto.macNew("HmacSHA256", slice(ascii("k")));
        GoCrypto.macFree(id);
        try {
            GoCrypto.macWrite(id, slice(ascii("x")));
            throw new AssertionError("macWrite on a freed handle must throw");
        } catch (IllegalArgumentException expected) {
            // Expected.
        }
        try {
            GoCrypto.macNew("HmacNOPE", slice(ascii("k")));
            throw new AssertionError("unknown MAC algorithm must throw");
        } catch (IllegalArgumentException expected) {
            // Expected.
        }
    }

    public void testMacSize() {
        assertEq(32, GoCrypto.macSize("HmacSHA256"), "HmacSHA256 size");
    }

    public void testGoBytesNullIsEmpty() {
        // GoCrypto.sum with a nil slice must hash the empty string, not throw.
        assertEq("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                hex(bytesOf(GoCrypto.sum("SHA-256", null))),
                "nil input must hash as empty");
    }
}
