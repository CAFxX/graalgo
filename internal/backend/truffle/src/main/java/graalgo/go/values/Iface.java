package graalgo.go.values;

import graalgo.go.types.GoType;

/**
 * A non-nil Go interface value: dynamic type descriptor and data (SPEC §3.4). The nil interface
 * is Java {@code null}. {@code data} is the value in its carrier form, boxed: {@code Long} for
 * every integer type, {@code Double} for floats, {@code Boolean}, or the reference; struct and
 * array values are a private {@link Flat} copy that is never mutated.
 */
public final class Iface {
    public final GoType type;
    public final Object data;
    /**
     * The data word of a non-pointer-shaped scalar value read through {@code unsafe} (gc: the
     * pointer to the boxed value), made once: gc's data word is one pointer for the value's
     * life, and code compares it ({@code atomic.Value.CompareAndSwap} CASes the word it read).
     * Guarded by {@code this}; see {@code Unsafe.dataWord}.
     */
    private Object word;

    public Iface(GoType type, Object data) {
        this.type = type;
        this.data = data;
    }

    /** The cached data word, or null before the first read. */
    public synchronized Object word() {
        return word;
    }

    /** Caches {@code w} unless a word is cached already; returns the word in effect. */
    public synchronized Object cacheWord(Object w) {
        if (word == null) {
            word = w;
        }
        return word;
    }
}
