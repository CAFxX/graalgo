package graalgo.go.runtime;

import java.lang.ref.WeakReference;
import java.util.Arrays;

/**
 * The range-allocating handle table behind {@code uintptr(p)} (SPEC §3.4, GG-0062): the first
 * conversion of a pointer into an object gives the object a range of integers as long as the
 * object is in gc bytes; {@code uintptr(p)} is the range base plus the byte offset, and an
 * integer converts back by a floor lookup. Ranges start where gc's heap addresses do, never
 * overlap, and are separated by at least one unassigned integer, so an integer one past an
 * object's end resolves to that object's end and nothing further out resolves at all.
 *
 * <p>The table holds objects weakly: as in gc, an integer does not keep its object alive, and
 * an integer whose object is gone resolves to nothing.
 *
 * <p>Objects are found by identity in an open-addressing table of the ranges themselves (one
 * weak reference per range, no reference queue). Bases only grow, so the ranges are an append-only sorted array searched by bisection (the
 * newest range first: a {@code uintptr} round trip such as {@code abi.NoEscape}, taken on every
 * {@code strings.Builder} and {@code fmt} printer, converts back the range it just made). Dead
 * ranges are squeezed out when the array is full, before it grows: amortized O(1) per range.
 * (A tree with a periodic sweep of every range cost O(live ranges) per 4,096 conversions,
 * quadratic between collections: round times grew with the heap.) A range whose object died
 * and is not squeezed out yet resolves to nothing, as a removed one would: the integer lies
 * past the end of every earlier range.
 */
public final class Handles {
    /** One object's range; also its entry in the identity table {@link #byObj}. */
    private static final class Range extends WeakReference<Object> {
        final long base;
        final long extent;
        final int hash;

        Range(Object obj, int hash, long base, long extent) {
            super(obj);
            this.hash = hash;
            this.base = base;
            this.extent = extent;
        }
    }

    /** Object to range: open addressing on the identity hash; dead entries go at rehash. */
    private Range[] byObj = new Range[1024];
    private int byObjUsed;
    /** The ranges in base order. */
    private long[] keys = new long[256];
    private Range[] refs = new Range[256];
    private int size;
    /** The first handle: every handle is at or above it (the pc range of {@link Stacks} is below). */
    public static final long FIRST = 0xc000000000L;

    private long next = FIRST;

    private static int hash(Object obj) {
        int h = System.identityHashCode(obj);
        return h ^ (h >>> 16);
    }

    /** The range base of {@code obj}, an object of {@code extent} gc bytes (identity-keyed). */
    public synchronized long base(Object obj, long extent) {
        int h = hash(obj);
        Range[] t = byObj;
        int mask = t.length - 1;
        int i = h & mask;
        for (Range r; (r = t[i]) != null; i = (i + 1) & mask) {
            if (r.hash == h && r.get() == obj) {
                return r.base;
            }
        }
        long b = next;
        next += (extent + 16) & ~15L;
        Range r = new Range(obj, h, b, extent);
        if ((byObjUsed + 1) * 2 > t.length) {
            rehash();
            t = byObj;
            mask = t.length - 1;
            i = h & mask;
            while (t[i] != null) {
                i = (i + 1) & mask;
            }
        }
        t[i] = r;
        byObjUsed++;
        if (size == keys.length) {
            makeRoom();
        }
        keys[size] = b;
        refs[size] = r;
        size++;
        return b;
    }

    /** Rebuilds the identity table from its live entries, at most half full afterwards. */
    private void rehash() {
        Range[] old = byObj;
        int live = 0;
        for (Range r : old) {
            if (r != null && r.get() != null) {
                live++;
            }
        }
        int cap = 1024;
        while (cap < live * 4) {
            cap <<= 1;
        }
        Range[] t = new Range[cap];
        int mask = cap - 1;
        for (Range r : old) {
            if (r != null && r.get() != null) {
                int i = r.hash & mask;
                while (t[i] != null) {
                    i = (i + 1) & mask;
                }
                t[i] = r;
            }
        }
        byObj = t;
        byObjUsed = live;
    }

    /** Drops the dead ranges of the base-ordered array; grows it when that frees less than half. */
    private void makeRoom() {
        int n = 0;
        for (int i = 0; i < size; i++) {
            if (refs[i].get() != null) {
                keys[n] = keys[i];
                refs[n] = refs[i];
                n++;
            }
        }
        Arrays.fill(refs, n, size, null);
        size = n;
        if (n * 2 >= keys.length) {
            keys = Arrays.copyOf(keys, keys.length * 2);
            refs = Arrays.copyOf(refs, refs.length * 2);
        }
    }

    /** The live object whose range (its end included) contains {@code addr} and the offset, or null. */
    public synchronized Object[] resolve(long addr) {
        int i = size - 1;
        if (i < 0 || addr < keys[0]) {
            return null;
        }
        if (addr < keys[i]) {
            i = Arrays.binarySearch(keys, 0, size, addr);
            if (i < 0) {
                i = -i - 2;
            }
        }
        Range r = refs[i];
        Object obj = r.get();
        long off = addr - r.base;
        return obj == null || off > r.extent ? null : new Object[] {obj, off};
    }
}
