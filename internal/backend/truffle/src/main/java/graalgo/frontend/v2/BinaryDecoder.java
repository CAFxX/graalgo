package graalgo.frontend.v2;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Decodes the v2 Go→Java binary interface format.
 *
 * <p>The v2 frontend (Go) encodes the in-memory {@code Document} into a
 * compact binary format (see {@code internal/frontend/v2/encode/encode.go}).
 * This decoder reconstructs the generic structure (maps, lists, strings,
 * longs) which {@link NodeFactoryV2} then converts into Truffle nodes.
 *
 * <p>Format (all integers are unsigned varints, strings are UTF-8):
 * <pre>
 *   document := magic version value
 *   magic    := 0x67 0x32 0x62 0x69 ("g2bi": Go→Java binary interface)
 *   version  := varint format version (currently 1)
 *   value    := tag payload
 *   tag      := 0x00 (null) | 0x01 (bool) | 0x02 (int) | 0x03 (string)
 *              | 0x04 (list) | 0x05 (map)
 *   int      := varint of the zigzag-encoded 64-bit pattern:
 *               zz(x) = (x &lt;&lt; 1) ^ (x &gt;&gt; 63) on the bit pattern.
 *               The Go encoder zigzag-encodes EVERY integer width
 *               (int..int64, uint..uint64), so every TAG_INT is un-zigzagged
 *               unconditionally here. Decoded values are the original 64-bit
 *               patterns, surfaced as signed longs.
 * </pre>
 *
 * <p>The magic + version header lets the decoder reject version skew,
 * truncation, or garbage input loudly instead of misdecoding it.
 *
 * <p>This is NOT JSON. The binary format is a proper Go→Java interface:
 * compact, schema-driven, and without the parsing overhead of JSON.
 */
public final class BinaryDecoder {

    /** Wire magic identifying the v2 Go→Java binary interface ("g2bi"). */
    private static final byte[] MAGIC = {'g', '2', 'b', 'i'};

    /**
     * Current wire format version. Bump on any incompatible change, in
     * lockstep with the Go encoder's formatVersion
     * (internal/frontend/v2/encode/encode.go).
     */
    private static final long FORMAT_VERSION = 1;

    private static final int TAG_NULL = 0x00;
    private static final int TAG_BOOL = 0x01;
    private static final int TAG_INT = 0x02;
    private static final int TAG_STRING = 0x03;
    private static final int TAG_LIST = 0x04;
    private static final int TAG_MAP = 0x05;

    /**
     * Read chunk size for length-prefixed payloads. The wire length is
     * attacker-controlled, so buffers are never pre-sized from it; they
     * grow in chunks of this size as real input arrives.
     */
    private static final int READ_CHUNK = 8192;

    private BinaryDecoder() {}

    /**
     * Decodes a full binary document into a {@code Map<String, Object>}.
     * The top-level value must be a map (the Document).
     *
     * <p>Validates the magic + version header first: version skew,
     * truncation, or garbage input fails loudly here instead of
     * misdecoding.
     */
    @SuppressWarnings("unchecked")
    public static Map<String, Object> decodeDocument(byte[] data) throws IOException {
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(data));
        byte[] magic = new byte[MAGIC.length];
        try {
            in.readFully(magic);
        } catch (EOFException e) {
            throw new IOException(
                "v2 binary: truncated header (expected 4 magic bytes + version)", e);
        }
        if (!Arrays.equals(magic, MAGIC)) {
            throw new IOException("v2 binary: bad magic " + hexBytes(magic)
                + ", want \"g2bi\" — not a v2 binary document");
        }
        long version;
        try {
            version = readVarint(in);
        } catch (EOFException e) {
            throw new IOException("v2 binary: truncated header (missing version)", e);
        }
        if (version != FORMAT_VERSION) {
            throw new IOException("v2 binary: unsupported format version " + version
                + " (this decoder supports " + FORMAT_VERSION + ")");
        }
        Object v = decodeValue(in);
        if (!(v instanceof Map)) {
            throw new IOException("v2 binary: top-level must be a map, got " +
                (v == null ? "null" : v.getClass().getSimpleName()));
        }
        return (Map<String, Object>) v;
    }

    private static String hexBytes(byte[] b) {
        StringBuilder sb = new StringBuilder();
        for (byte x : b) {
            sb.append(String.format("%02x", x));
        }
        return sb.toString();
    }

    /**
     * Decodes one value from the stream.
     *
     * @return null, Boolean, Long, String, List&lt;Object&gt;, or Map&lt;String, Object&gt;
     */
    public static Object decodeValue(DataInputStream in) throws IOException {
        int tag;
        try {
            tag = in.readUnsignedByte();
        } catch (EOFException e) {
            throw new IOException("v2 binary: truncated value (missing tag byte)", e);
        }
        switch (tag) {
            case TAG_NULL:
                return null;
            case TAG_BOOL:
                return in.readUnsignedByte() != 0;
            case TAG_INT:
                return readZigzagVarint(in);
            case TAG_STRING: {
                byte[] bytes = readSizedBytes(in, "string");
                return new String(bytes, StandardCharsets.UTF_8);
            }
            case TAG_LIST: {
                long count = readVarint(in);
                if (count > Integer.MAX_VALUE) {
                    throw new IOException("v2 binary: list too long: " + count);
                }
                // Never pre-size from the wire count: a few input bytes can
                // claim up to 2^31-1 elements. The list grows as real
                // elements decode; short input ends the loop at EOF below.
                List<Object> list = new ArrayList<>();
                for (long i = 0; i < count; i++) {
                    list.add(decodeValue(in));
                }
                return list;
            }
            case TAG_MAP: {
                long count = readVarint(in);
                if (count > Integer.MAX_VALUE) {
                    throw new IOException("v2 binary: map too large: " + count);
                }
                Map<String, Object> map = new LinkedHashMap<>();
                for (long i = 0; i < count; i++) {
                    byte[] keyBytes = readSizedBytes(in, "map key");
                    String key = new String(keyBytes, StandardCharsets.UTF_8);
                    Object val = decodeValue(in);
                    map.put(key, val);
                }
                return map;
            }
            default:
                throw new IOException(String.format("v2 binary: unknown tag 0x%02x", tag));
        }
    }

    /**
     * Reads a length-prefixed byte string from the stream.
     *
     * <p>The claimed length is attacker-controlled (up to 2^31-1 from a few
     * wire bytes), so the buffer is never pre-sized from it: bytes are read
     * in fixed-size chunks and the buffer grows only as real input arrives.
     * A claimed length that exceeds the remaining input fails with a decode
     * error, never with an OutOfMemoryError.
     */
    private static byte[] readSizedBytes(DataInputStream in, String what) throws IOException {
        long len = readVarint(in);
        if (len > Integer.MAX_VALUE) {
            throw new IOException("v2 binary: " + what + " too long: " + len);
        }
        long remaining = len;
        ByteArrayOutputStream out =
            new ByteArrayOutputStream((int) Math.min(remaining, READ_CHUNK));
        byte[] chunk = new byte[READ_CHUNK];
        while (remaining > 0) {
            int n = (int) Math.min(remaining, READ_CHUNK);
            try {
                in.readFully(chunk, 0, n);
            } catch (EOFException e) {
                throw new IOException("v2 binary: truncated " + what
                    + " (claimed " + len + " bytes, input ended early)", e);
            }
            out.write(chunk, 0, n);
            remaining -= n;
        }
        return out.toByteArray();
    }

    /**
     * Reads an unsigned varint (LEB128).
     */
    private static long readVarint(DataInputStream in) throws IOException {
        long result = 0;
        int shift = 0;
        while (true) {
            int b = in.readUnsignedByte();
            result |= ((long) (b & 0x7F)) << shift;
            if ((b & 0x80) == 0) {
                break;
            }
            shift += 7;
            if (shift >= 64) {
                throw new IOException("v2 binary: varint too long");
            }
        }
        return result;
    }

    /**
     * Reads a zigzag-encoded 64-bit pattern (varint).
     *
     * <p>The Go encoder zigzag-encodes every integer width, so every
     * TAG_INT is inverted here unconditionally. The result is the original
     * 64-bit pattern as a signed long.
     */
    private static long readZigzagVarint(DataInputStream in) throws IOException {
        long n = readVarint(in);
        return (n >>> 1) ^ -(n & 1);
    }
}
