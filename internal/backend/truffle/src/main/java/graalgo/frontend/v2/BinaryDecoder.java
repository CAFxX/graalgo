package graalgo.frontend.v2;

import java.io.ByteArrayInputStream;
import java.io.DataInputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
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
 *   value := tag payload
 *   tag   := 0x00 (null) | 0x01 (bool) | 0x02 (int) | 0x03 (string)
 *          | 0x04 (list) | 0x05 (map)
 * </pre>
 *
 * <p>This is NOT JSON. The binary format is a proper Go→Java interface:
 * compact, schema-driven, and without the parsing overhead of JSON.
 */
public final class BinaryDecoder {

    private static final int TAG_NULL = 0x00;
    private static final int TAG_BOOL = 0x01;
    private static final int TAG_INT = 0x02;
    private static final int TAG_STRING = 0x03;
    private static final int TAG_LIST = 0x04;
    private static final int TAG_MAP = 0x05;

    private BinaryDecoder() {}

    /**
     * Decodes a full binary document into a {@code Map<String, Object>}.
     * The top-level value must be a map (the Document).
     */
    @SuppressWarnings("unchecked")
    public static Map<String, Object> decodeDocument(byte[] data) throws IOException {
        Object v = decodeValue(new DataInputStream(new ByteArrayInputStream(data)));
        if (!(v instanceof Map)) {
            throw new IOException("v2 binary: top-level must be a map, got " +
                (v == null ? "null" : v.getClass().getSimpleName()));
        }
        return (Map<String, Object>) v;
    }

    /**
     * Decodes one value from the stream.
     *
     * @return null, Boolean, Long, String, List&lt;Object&gt;, or Map&lt;String, Object&gt;
     */
    public static Object decodeValue(DataInputStream in) throws IOException {
        int tag = in.readUnsignedByte();
        switch (tag) {
            case TAG_NULL:
                return null;
            case TAG_BOOL:
                return in.readUnsignedByte() != 0;
            case TAG_INT:
                return readZigzagVarint(in);
            case TAG_STRING: {
                long len = readVarint(in);
                if (len > Integer.MAX_VALUE) {
                    throw new IOException("v2 binary: string too long: " + len);
                }
                byte[] bytes = new byte[(int) len];
                in.readFully(bytes);
                return new String(bytes, StandardCharsets.UTF_8);
            }
            case TAG_LIST: {
                long count = readVarint(in);
                if (count > Integer.MAX_VALUE) {
                    throw new IOException("v2 binary: list too long: " + count);
                }
                List<Object> list = new ArrayList<>((int) count);
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
                    long keyLen = readVarint(in);
                    if (keyLen > Integer.MAX_VALUE) {
                        throw new IOException("v2 binary: key too long: " + keyLen);
                    }
                    byte[] keyBytes = new byte[(int) keyLen];
                    in.readFully(keyBytes);
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
     * Reads a zigzag-encoded signed varint.
     */
    private static long readZigzagVarint(DataInputStream in) throws IOException {
        long n = readVarint(in);
        return (n >>> 1) ^ -(n & 1);
    }
}
