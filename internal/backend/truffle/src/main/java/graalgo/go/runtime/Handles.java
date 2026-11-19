package graalgo.go.runtime;

import java.lang.ref.WeakReference;
import java.util.Map;
import java.util.TreeMap;
import java.util.WeakHashMap;

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
 */
public final class Handles {
    private record Range(WeakReference<Object> obj, long extent) {}

    private final WeakHashMap<Object, Long> bases = new WeakHashMap<>();
    private final TreeMap<Long, Range> ranges = new TreeMap<>();
    private long next = 0xc000000000L;
    private int sinceSweep;

    /** The range base of {@code obj}, an object of {@code extent} gc bytes (identity-keyed). */
    public synchronized long base(Object obj, long extent) {
        Long b = bases.get(obj);
        if (b == null) {
            b = next;
            next += (extent + 16) & ~15L;
            bases.put(obj, b);
            ranges.put(b, new Range(new WeakReference<>(obj), extent));
            if (++sinceSweep >= 4096) {
                sinceSweep = 0;
                ranges.values().removeIf(r -> r.obj.get() == null);
            }
        }
        return b;
    }

    /** The live object whose range (its end included) contains {@code addr} and the offset, or null. */
    public synchronized Object[] resolve(long addr) {
        Map.Entry<Long, Range> e = ranges.floorEntry(addr);
        if (e == null) {
            return null;
        }
        Object obj = e.getValue().obj.get();
        long off = addr - e.getKey();
        return obj == null || off > e.getValue().extent ? null : new Object[] {obj, off};
    }
}
