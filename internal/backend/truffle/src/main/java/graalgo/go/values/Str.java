package graalgo.go.values;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/**
 * An immutable Go string: bytes, not necessarily UTF-8 (SPEC §3.4). {@code b} is never written
 * after construction and never handed out for writing.
 */
public final class Str {
    public static final Str EMPTY = new Str(new byte[0]);

    public final byte[] b;

    /** Wraps {@code b}; the caller gives up the array. */
    public Str(byte[] b) {
        this.b = b;
    }

    public static Str of(String s) {
        return s.isEmpty() ? EMPTY : new Str(s.getBytes(StandardCharsets.UTF_8));
    }

    public static boolean eq(Str a, Str b) {
        return a == b || Arrays.equals(a.b, b.b);
    }

    /** Byte-wise comparison, as Go's string ordering. */
    public static int cmp(Str a, Str b) {
        return Arrays.compareUnsigned(a.b, b.b);
    }

    public static Str concat(Str a, Str b) {
        if (a.b.length == 0) {
            return b;
        }
        if (b.b.length == 0) {
            return a;
        }
        byte[] out = Arrays.copyOf(a.b, a.b.length + b.b.length);
        System.arraycopy(b.b, 0, out, a.b.length, b.b.length);
        return new Str(out);
    }

    @Override
    public boolean equals(Object o) {
        return o instanceof Str s && Arrays.equals(b, s.b);
    }

    @Override
    public int hashCode() {
        return Arrays.hashCode(b);
    }

    @Override
    public String toString() {
        return new String(b, StandardCharsets.UTF_8);
    }
}
