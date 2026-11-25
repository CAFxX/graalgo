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
    /**
     * The storage as an addressable object, set by the first {@code unsafe.StringData} of this
     * string (or the object {@code unsafe.String} made it over): a cache, benign when raced.
     */
    public Flat flat;

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

    /**
     * The process-wide key of the byte hash, drawn at start-up (F-9.3): a program cannot
     * precompute colliding strings offline. Maps add their own per-map seed on top
     * ({@link GoMap}), as gc combines its per-process aeskey with each map's seed.
     */
    private static final long HASH_KEY = java.util.concurrent.ThreadLocalRandom.current().nextLong();

    /**
     * {@link #hash()}'s cache: 0 until computed (a computed hash is never 0, so the field keeps
     * the object at 32 bytes). Benign races, as {@code String.hash}.
     */
    private int hash;

    /**
     * The keyed hash of the bytes, cached in the string (Go strings are immutable, ADR-0015
     * decision 3): a map lookup with a string that was hashed before does not rescan it.
     */
    public int hash() {
        int h = hash;
        if (h == 0) {
            h = computeHash();
            hash = h;
        }
        return h;
    }

    private int computeHash() {
        byte[] a = b;
        long h = HASH_KEY ^ len;
        int i = off;
        int end = off + len;
        for (; i + 8 <= end; i += 8) {
            long w = (a[i] & 0xFFL) | (a[i + 1] & 0xFFL) << 8 | (a[i + 2] & 0xFFL) << 16 | (a[i + 3] & 0xFFL) << 24
                    | (a[i + 4] & 0xFFL) << 32 | (a[i + 5] & 0xFFL) << 40 | (a[i + 6] & 0xFFL) << 48
                    | (long) a[i + 7] << 56;
            h = (h ^ w * 0x9E3779B97F4A7C15L) * 0xBF58476D1CE4E5B9L;
            h ^= h >>> 31;
        }
        for (; i < end; i++) {
            h = (h ^ (a[i] & 0xFFL)) * 0x100000001B3L;
        }
        h ^= h >>> 33;
        h *= 0xFF51AFD7ED558CCDL;
        h ^= h >>> 33;
        int r = (int) (h ^ h >>> 32);
        return r != 0 ? r : 1;
    }

    @Override
    public int hashCode() {
        return hash();
    }

    @Override
    public String toString() {
        return new String(b, off, len, StandardCharsets.UTF_8);
    }
}
