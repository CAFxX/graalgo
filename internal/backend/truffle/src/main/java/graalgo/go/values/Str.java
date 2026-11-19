package graalgo.go.values;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/**
 * An immutable Go string: {@code len} bytes of {@code b} from {@code off}, not necessarily
 * UTF-8 (SPEC §3.4). Substrings share the array, as in gc. The bytes of a string are never
 * written through it; {@code unsafe.String} can make a string over storage a slice also
 * addresses, with gc's contract that the program does not modify it (ADR-0015 decision 3).
 */
public final class Str {
    public static final Str EMPTY = new Str(new byte[0]);

    public final byte[] b;
    public final int off;
    public final int len;

    /** Wraps {@code b}; the caller gives up the array. */
    public Str(byte[] b) {
        this(b, 0, b.length);
    }

    public Str(byte[] b, int off, int len) {
        this.b = b;
        this.off = off;
        this.len = len;
    }

    public static Str of(String s) {
        return s.isEmpty() ? EMPTY : new Str(s.getBytes(StandardCharsets.UTF_8));
    }

    /** Byte {@code i} of the string, unsigned. */
    public int at(int i) {
        return b[off + i] & 0xFF;
    }

    /** The bytes as a fresh array. */
    public byte[] toBytes() {
        return Arrays.copyOfRange(b, off, off + len);
    }

    public static boolean eq(Str a, Str b) {
        return a == b || a.len == b.len && Arrays.equals(a.b, a.off, a.off + a.len, b.b, b.off, b.off + b.len);
    }

    /** Byte-wise comparison, as Go's string ordering. */
    public static int cmp(Str a, Str b) {
        return Arrays.compareUnsigned(a.b, a.off, a.off + a.len, b.b, b.off, b.off + b.len);
    }

    public static Str concat(Str a, Str b) {
        if (a.len == 0) {
            return b;
        }
        if (b.len == 0) {
            return a;
        }
        byte[] out = new byte[a.len + b.len];
        System.arraycopy(a.b, a.off, out, 0, a.len);
        System.arraycopy(b.b, b.off, out, a.len, b.len);
        return new Str(out);
    }

    @Override
    public boolean equals(Object o) {
        return o instanceof Str s && eq(this, s);
    }

    @Override
    public int hashCode() {
        int h = 1;
        for (int i = off; i < off + len; i++) {
            h = 31 * h + b[i];
        }
        return h;
    }

    @Override
    public String toString() {
        return new String(b, off, len, StandardCharsets.UTF_8);
    }
}
