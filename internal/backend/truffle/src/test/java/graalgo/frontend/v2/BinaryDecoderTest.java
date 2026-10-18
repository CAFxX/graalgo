package graalgo.frontend.v2;

import java.io.IOException;
import java.util.List;
import java.util.Map;

/**
 * Cross-language contract tests for the v2 Go→Java binary interface.
 *
 * <p>The GOLDEN hex vectors below were produced by the Go encoder
 * ({@code internal/frontend/v2/encode}, {@code TestEncodeIntContract} and
 * {@code TestEncodeGoldenDoc}): run
 * {@code cd internal/frontend/v2 && go test ./encode/ -v} to regenerate.
 * Every integer width is zigzag-encoded by the Go encoder and un-zigzagged
 * here; decoded values are the original 64-bit patterns as signed longs.
 *
 * <p>A change to any golden below means the wire format changed: bump
 * {@code BinaryDecoder} FORMAT_VERSION and the Go encoder's
 * {@code formatVersion} together, and update both format spec comments.
 */
public final class BinaryDecoderTest {

    private static final String MAGIC_HEX = "67326269"; // "g2bi"
    private static final String VERSION_HEX = "02"; // version 2

    /**
     * Golden document, byte-identical to the Go
     * {@code TestEncodeGoldenDoc} output: magic + version + map with keys
     * "big" &lt; "l" &lt; "neg" &lt; "s".
     */
    private static final String GOLDEN_DOC_HEX =
        MAGIC_HEX + VERSION_HEX
        + "0504" // map, 4 entries
        + "03626967" + "02ffffffffffffffffff01" // "big": uint64(1)<<63
        + "016c" + "0402" + "0202" + "030374776f" // "l": [1, "two"]
        + "036e6567" + "0209" // "neg": int64(-5)
        + "0173" + "03026869"; // "s": "hi"

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static byte[] hex(String s) {
        check(s.length() % 2 == 0, "odd hex length");
        byte[] out = new byte[s.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    /** Wraps one TAG_INT payload in a single-entry map document. */
    private static byte[] intDoc(String intHex) {
        // map{"v": <int>}, with header
        return hex(MAGIC_HEX + VERSION_HEX + "0501" + "0176" + intHex);
    }

    private static long decodeInt(String intHex) throws IOException {
        Map<String, Object> doc = BinaryDecoder.decodeDocument(intDoc(intHex));
        Object v = doc.get("v");
        check(v instanceof Long, "expected Long, got " + v);
        return (Long) v;
    }

    public void testZigzagGoldens() throws IOException {
        // Mirrors Go TestEncodeIntContract byte-for-byte.
        check(decodeInt("0200") == 0L, "int 0");
        check(decodeInt("0202") == 1L, "int 1");
        check(decodeInt("0201") == -1L, "int -1");
        check(decodeInt("0254") == 42L, "int 42");
        check(decodeInt("0221") == -17L, "int -17");
        check(decodeInt("02ff01") == -128L, "int8 min");
        check(decodeInt("02cf0f") == -1000L, "int16 -1000");
        check(decodeInt("02ffffffff0f") == -2147483648L, "int32 min");
        check(decodeInt("02ffffffffffffffffff01") == Long.MIN_VALUE, "int64 min");
        check(decodeInt("02feffffffffffffffff01") == Long.MAX_VALUE, "int64 max");
        check(decodeInt("02fe03") == 255L, "uint8 max");
        check(decodeInt("02feff07") == 65535L, "uint16 max");
        check(decodeInt("02feffffff1f") == 4294967295L, "uint32 max");
        // R5-P1-1 regressions: these were silently misdecoded before the fix.
        check(decodeInt("020a") == 5L, "uint64(5)");
        check(decodeInt("02ffffffffffffffffff01") == Long.MIN_VALUE,
            "uint64(1)<<63 pattern");
        check(decodeInt("0201") == -1L, "uint64 max pattern");
    }

    public void testGoldenDocument() throws IOException {
        Map<String, Object> doc = BinaryDecoder.decodeDocument(hex(GOLDEN_DOC_HEX));
        check(doc.size() == 4, "4 entries, got " + doc.size());
        check(((Long) doc.get("big")).longValue() == Long.MIN_VALUE,
            "big = uint64(1)<<63 pattern");
        check(((Long) doc.get("neg")).longValue() == -5L, "neg = -5");
        check("hi".equals(doc.get("s")), "s = hi");
        @SuppressWarnings("unchecked")
        List<Object> l = (List<Object>) doc.get("l");
        check(l.size() == 2, "l has 2 elements");
        check(((Long) l.get(0)).longValue() == 1L, "l[0] = 1");
        check("two".equals(l.get(1)), "l[1] = two");
    }

    public void testBadMagicRejected() {
        // Bogus magic bytes + version + empty map.
        byte[] bad = hex("58585858" + VERSION_HEX + "0500");
        try {
            BinaryDecoder.decodeDocument(bad);
        } catch (IOException e) {
            check(e.getMessage().contains("bad magic"),
                "message names bad magic, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("bad magic not rejected");
    }

    public void testBadVersionRejected() {
        // Valid magic, version 99, empty map.
        byte[] bad = hex(MAGIC_HEX + "63" + "0500");
        try {
            BinaryDecoder.decodeDocument(bad);
        } catch (IOException e) {
            check(e.getMessage().contains("unsupported format version 99"),
                "message names version, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("bad version not rejected");
    }

    public void testTruncatedHeaderRejected() {
        try {
            BinaryDecoder.decodeDocument(hex("6732"));
        } catch (IOException e) {
            check(e.getMessage().contains("truncated header"),
                "message names truncation, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("truncated magic not rejected");
    }

    public void testTruncatedVersionRejected() {
        try {
            BinaryDecoder.decodeDocument(hex(MAGIC_HEX));
        } catch (IOException e) {
            check(e.getMessage().contains("truncated header"),
                "message names truncation, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("truncated version not rejected");
    }

    public void testTopLevelMustBeMap() {
        // Valid header, then a bare int instead of a map.
        byte[] bad = hex(MAGIC_HEX + VERSION_HEX + "0202");
        try {
            BinaryDecoder.decodeDocument(bad);
        } catch (IOException e) {
            check(e.getMessage().contains("top-level must be a map"),
                "message names top-level, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("non-map top-level not rejected");
    }

    // --- Untrusted-length hardening (V2-22) ---
    //
    // Lengths/counts on the wire are attacker-controlled: a few input bytes
    // can claim a 2^31-1-byte string or a 2^31-1-element list. These inputs
    // must fail with a clean decode IOException, never with OutOfMemoryError
    // (or by allocating gigabytes before failing).

    /** Wraps a raw value payload in header + map{"v": <value>}. */
    private static byte[] valueDoc(String valueHex) {
        return hex(MAGIC_HEX + VERSION_HEX + "0501" + "0176" + valueHex);
    }

    private static void expectCleanDecodeError(byte[] doc, String what) {
        try {
            BinaryDecoder.decodeDocument(doc);
        } catch (IOException e) {
            return; // clean decode error: the required behavior
        }
        throw new AssertionError(what + " not rejected");
    }

    public void testHugeStringLengthFailsCleanly() {
        // TAG_STRING claiming 2^31-1 bytes, with zero payload bytes present.
        expectCleanDecodeError(valueDoc("03" + "ffffffff07"), "huge string length");
    }

    public void testHugeListCountFailsCleanly() {
        // TAG_LIST claiming 2^31-1 elements, with zero element bytes present.
        expectCleanDecodeError(valueDoc("04" + "ffffffff07"), "huge list count");
    }

    public void testHugeMapKeyLengthFailsCleanly() {
        // TAG_MAP with 1 entry whose key claims 2^31-1 bytes, none present.
        expectCleanDecodeError(valueDoc("05" + "01" + "ffffffff07"), "huge map key length");
    }

    public void testTruncatedStringPayloadFailsCleanly() {
        // TAG_STRING claiming 100 bytes, only 3 ("abc") present.
        byte[] bad = valueDoc("03" + "64" + "616263");
        try {
            BinaryDecoder.decodeDocument(bad);
        } catch (IOException e) {
            check(e.getMessage().contains("truncated"),
                "message names truncation, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("truncated string payload not rejected");
    }

    public void testEmptyContainersDecode() throws IOException {
        // Positive control: legitimately empty shapes still decode fine.
        Map<String, Object> s = BinaryDecoder.decodeDocument(valueDoc("03" + "00"));
        check("".equals(s.get("v")), "empty string decodes");
        @SuppressWarnings("unchecked")
        List<Object> l = (List<Object>) BinaryDecoder.decodeDocument(
            valueDoc("04" + "00")).get("v");
        check(l.isEmpty(), "empty list decodes");
        Map<String, Object> m = BinaryDecoder.decodeDocument(valueDoc("05" + "00"));
        check(m.get("v") instanceof Map, "empty map decodes");
    }
}
