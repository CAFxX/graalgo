package graalgo.values;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/**
 * A Go string value: an immutable, length-delimited byte sequence (D5).
 *
 * <p>Go strings are arbitrary bytes, not necessarily valid UTF-8; equality
 * and ordering are byte-wise. Comparison ({@link #compareTo}) is unsigned
 * lexicographic on bytes, matching Go's string comparison semantics
 * (verified against stock gc: {@code "abc" < "abd"}, {@code "" < "a"}).
 */
public final class GoString implements Comparable<GoString> {
    /** The Go zero value for strings: the empty (non-nil) string. */
    public static final GoString EMPTY = new GoString(new byte[0]);

    private final byte[] bytes;

    public GoString(byte[] bytes) {
        this.bytes = bytes.clone();
    }

    /** Builds a GoString from the UTF-8 encoding of {@code s}. */
    public static GoString fromUtf8(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    /**
     * The Go {@code string(rune)} conversion: the UTF-8 encoding of the code
     * point, or U+FFFD for values outside the valid range (negative,
     * surrogates, &gt; 0x10FFFF) — verified against stock gc.
     */
    public static GoString fromRune(int codePoint) {
        if (codePoint < 0 || codePoint > 0x10FFFF
                || (codePoint >= 0xD800 && codePoint <= 0xDFFF)) {
            return fromUtf8("\uFFFD");
        }
        return fromUtf8(new String(Character.toChars(codePoint)));
    }

    /** Defensive copy of the underlying bytes. */
    public byte[] bytes() {
        return bytes.clone();
    }

    public int length() {
        return bytes.length;
    }

    /** Concatenation ({@code +} on Go strings). */
    public GoString concat(GoString other) {
        byte[] out = Arrays.copyOf(bytes, bytes.length + other.bytes.length);
        System.arraycopy(other.bytes, 0, out, bytes.length, other.bytes.length);
        return new GoString(out);
    }

    @Override
    public int compareTo(GoString other) {
        int n = Math.min(bytes.length, other.bytes.length);
        for (int i = 0; i < n; i++) {
            int a = bytes[i] & 0xFF;
            int b = other.bytes[i] & 0xFF;
            if (a != b) {
                return a - b;
            }
        }
        return bytes.length - other.bytes.length;
    }

    @Override
    public boolean equals(Object o) {
        return o instanceof GoString gs && Arrays.equals(bytes, gs.bytes);
    }

    @Override
    public int hashCode() {
        return Arrays.hashCode(bytes);
    }

    /**
     * Best-effort decoding for debugging/display. Lossy for invalid UTF-8
     * (replacement chars); {@link #bytes()} is the exact value.
     */
    @Override
    public String toString() {
        return new String(bytes, StandardCharsets.UTF_8);
    }
}
