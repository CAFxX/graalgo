package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import java.io.ByteArrayOutputStream;

/**
 * String runtime helpers: UTF-8 decoding with Go's rules (an invalid sequence is U+FFFD of
 * width 1) and the string ↔ []byte / []rune conversions over flat slice storage.
 */
public final class Strs {
    private Strs() {}

    private static final int RUNE_ERROR = 0xFFFD;

    /** Decodes the rune at {@code i}: {@code (width << 32) | rune}. PE-safe (plain loads). */
    public static long decode(byte[] b, int i) {
        int c0 = b[i] & 0xFF;
        if (c0 < 0x80) {
            return (1L << 32) | c0;
        }
        return decodeMulti(b, i, c0);
    }

    private static long decodeMulti(byte[] b, int i, int c0) {
        int n = b.length - i;
        long err = (1L << 32) | RUNE_ERROR;
        if (c0 < 0xC2 || c0 > 0xF4 || n < 2) {
            return err;
        }
        int c1 = b[i + 1] & 0xFF;
        if (c0 < 0xE0) {
            return (c1 & 0xC0) != 0x80 ? err : (2L << 32) | ((c0 & 0x1F) << 6 | (c1 & 0x3F));
        }
        if (c0 < 0xF0) {
            int lo = c0 == 0xE0 ? 0xA0 : 0x80;
            int hi = c0 == 0xED ? 0x9F : 0xBF;
            if (c1 < lo || c1 > hi || n < 3 || (b[i + 2] & 0xC0) != 0x80) {
                return err;
            }
            return (3L << 32) | ((c0 & 0x0F) << 12 | (c1 & 0x3F) << 6 | (b[i + 2] & 0x3F));
        }
        int lo = c0 == 0xF0 ? 0x90 : 0x80;
        int hi = c0 == 0xF4 ? 0x8F : 0xBF;
        if (c1 < lo || c1 > hi || n < 4 || (b[i + 2] & 0xC0) != 0x80 || (b[i + 3] & 0xC0) != 0x80) {
            return err;
        }
        return (4L << 32) | ((c0 & 0x07) << 18 | (c1 & 0x3F) << 12 | (b[i + 2] & 0x3F) << 6 | (b[i + 3] & 0x3F));
    }

    private static void encode(ByteArrayOutputStream out, long rune) {
        int r = rune < 0 || rune > 0x10FFFF || (rune >= 0xD800 && rune <= 0xDFFF) ? RUNE_ERROR : (int) rune;
        if (r < 0x80) {
            out.write(r);
        } else if (r < 0x800) {
            out.write(0xC0 | r >> 6);
            out.write(0x80 | r & 0x3F);
        } else if (r < 0x10000) {
            out.write(0xE0 | r >> 12);
            out.write(0x80 | r >> 6 & 0x3F);
            out.write(0x80 | r & 0x3F);
        } else {
            out.write(0xF0 | r >> 18);
            out.write(0x80 | r >> 12 & 0x3F);
            out.write(0x80 | r >> 6 & 0x3F);
            out.write(0x80 | r & 0x3F);
        }
    }

    /** {@code string(rune)}. */
    @TruffleBoundary
    public static Str fromRune(long rune) {
        ByteArrayOutputStream out = new ByteArrayOutputStream(4);
        encode(out, rune);
        return new Str(out.toByteArray());
    }

    /** {@code string([]rune)}. */
    @TruffleBoundary
    public static Str fromRunes(Slice s) {
        ByteArrayOutputStream out = new ByteArrayOutputStream(s.len);
        for (int i = 0; i < s.len; i++) {
            encode(out, s.base.prims[s.prim + i]);
        }
        return new Str(out.toByteArray());
    }

    /** {@code []rune(string)}. */
    @TruffleBoundary
    public static Slice toRunes(Str s) {
        long[] tmp = new long[s.b.length];
        int n = 0;
        for (int i = 0; i < s.b.length;) {
            long d = decode(s.b, i);
            tmp[n++] = (int) d;
            i += (int) (d >>> 32);
        }
        return new Slice(new Flat(null, java.util.Arrays.copyOf(tmp, n), Flat.NO_REFS), 0, 0, n, n);
    }

    /** {@code string([]byte)}. */
    public static Str fromBytes(Slice s) {
        int n = s.len;
        if (n == 0) {
            return Str.EMPTY;
        }
        byte[] out = new byte[n];
        long[] src = s.base.prims;
        int off = s.prim;
        for (int i = 0; i < n; i++) {
            out[i] = (byte) src[off + i];
        }
        return new Str(out);
    }

    /** {@code []byte(string)}. */
    public static Slice toBytes(Str s) {
        int n = s.b.length;
        long[] out = new long[n];
        for (int i = 0; i < n; i++) {
            out[i] = s.b[i] & 0xFF;
        }
        return new Slice(new Flat(null, out, Flat.NO_REFS), 0, 0, n, n);
    }

    /** {@code s[lo:hi]} (bounds already checked). */
    public static Str slice(Str s, int lo, int hi) {
        return lo == 0 && hi == s.b.length ? s : new Str(java.util.Arrays.copyOfRange(s.b, lo, hi));
    }
}
