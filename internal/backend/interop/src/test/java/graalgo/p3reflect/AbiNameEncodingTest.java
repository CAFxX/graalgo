package graalgo.p3reflect;

import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;

/**
 * fix-redmaster-r40: unit tests for {@code AbiTypes.encodeName} — the
 * {@code internal/abi.NewName} byte encoding behind
 * {@code StructType.Field(i).Name} shells.
 *
 * <p>reflect's {@code Value.Field} calls {@code field.Name.IsExported()}
 * unconditionally; AbiTypes previously installed zero Names (nil Bytes)
 * and every struct Field call died in the compiled Name methods. The
 * encoding is gc's: a flags byte (bit0 exported, bit1 has-tag, bit3
 * embedded), a LEB128 varint name length, the name bytes, and — only
 * when a tag is present — a varint tag length and the tag bytes.
 *
 * <p>These tests invoke the private encoder reflectively and decode
 * the result exactly the way gc's {@code abi.Name} methods do, so a
 * wrong flag bit, a missing tag length, or a broken multi-byte varint
 * fails. Falsifiability: the encoder does not exist on the unfixed
 * base (the reflective lookup fails), and the long-name case pins the
 * two-byte varint form a single-byte writer would corrupt.
 */
public final class AbiNameEncodingTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static byte[] encode(String name, String tag, boolean exported, boolean embedded)
            throws Exception {
        Method m = AbiTypes.class.getDeclaredMethod("encodeName",
                String.class, String.class, boolean.class, boolean.class);
        m.setAccessible(true);
        return (byte[]) m.invoke(null, name, tag, exported, embedded);
    }

    /** A gc-style decoder for the abi.Name byte form. */
    private static final class Decoded {
        boolean exported;
        boolean hasTag;
        boolean embedded;
        String name;
        String tag = "";
    }

    private static long readVarint(byte[] b, int[] p) {
        long v = 0;
        int shift = 0;
        for (;;) {
            long cur = b[p[0]++] & 0xFF;
            v |= (cur & 0x7F) << shift;
            if ((cur & 0x80) == 0) {
                return v;
            }
            shift += 7;
        }
    }

    private static Decoded decode(byte[] b) {
        Decoded d = new Decoded();
        int flags = b[0] & 0xFF;
        d.exported = (flags & 1) != 0;
        d.hasTag = (flags & 2) != 0;
        d.embedded = (flags & 8) != 0;
        int[] p = {1};
        int nlen = (int) readVarint(b, p);
        d.name = new String(b, p[0], nlen, StandardCharsets.UTF_8);
        p[0] += nlen;
        if (d.hasTag) {
            int tlen = (int) readVarint(b, p);
            d.tag = new String(b, p[0], tlen, StandardCharsets.UTF_8);
            p[0] += tlen;
        }
        assertEquals(b.length, p[0], "decoder must consume exactly the encoding");
        return d;
    }

    public void testExportedPlainName() throws Exception {
        Decoded d = decode(encode("Age", "", true, false));
        assertTrue(d.exported, "exported flag must be set");
        assertTrue(!d.hasTag, "empty tag must clear the has-tag flag");
        assertTrue(!d.embedded, "embedded flag must be clear");
        assertTrue("Age".equals(d.name), "name round-trips, got " + d.name);
    }

    public void testUnexportedTaggedEmbedded() throws Exception {
        Decoded d = decode(encode("field", "json:\"f\"", false, true));
        assertTrue(!d.exported, "exported flag must be clear");
        assertTrue(d.hasTag, "has-tag flag must be set");
        assertTrue(d.embedded, "embedded flag must be set");
        assertTrue("field".equals(d.name), "name round-trips");
        assertTrue("json:\"f\"".equals(d.tag), "tag round-trips, got " + d.tag);
    }

    public void testLongNameUsesMultiByteVarint() throws Exception {
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < 300; i++) {
            sb.append('x');
        }
        String name = sb.toString();
        byte[] enc = encode(name, "", true, false);
        // 300 = 0b100101100 -> LEB128 [0xAC, 0x02]: two length bytes.
        assertEquals(0xAC, enc[1] & 0xFF, "first varint byte of a 300-byte name");
        assertEquals(0x02, enc[2] & 0xFF, "second varint byte of a 300-byte name");
        Decoded d = decode(enc);
        assertTrue(name.equals(d.name), "300-byte name round-trips");
    }

    public void testTagLengthIsEncodedNotAssumed() throws Exception {
        // A tag long enough to need its own two-byte varint: a decoder
        // reading a fixed one-byte tag length would desync and fail
        // the exact-consumption check in decode().
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < 200; i++) {
            sb.append('t');
        }
        Decoded d = decode(encode("N", sb.toString(), true, false));
        assertTrue(d.tag.length() == 200, "200-byte tag round-trips");
    }
}
