package abispike;

import abispike.Mem.Addr;
import abispike.Mem.Addressable;
import abispike.Mem.UnsafeTrap;
import java.util.IdentityHashMap;
import java.util.Map;
import java.util.TreeMap;

/**
 * {@code uintptr(unsafe.Pointer(p))} and back. Every object whose address
 * is taken as an integer gets a range in a private 62-bit "virtual address
 * space" (bump-allocated, 16-byte aligned, never reused); the integer is
 * {@code base + offset}. Consequences, all needed by verbatim std:
 * the same pointer always yields the same integer; integers of different
 * objects never collide; {@code uintptr(p) + n} stays inside the object's
 * range and converts back to {@code (object, offset + n)}; alignment tests
 * on the integer see the object's real field alignment; an integer that is
 * not inside any live range traps (GG-0016).
 *
 * <p>Prototype simplification: entries are strong. The backend keeps them
 * weak (the range is dropped when the object dies; Go's rule that the
 * pointer must be kept alive by the caller across the call makes that
 * safe).
 */
public final class HandleTable {
    /**
     * Above every user-space address Linux can hand out (2^47, or 2^56 with five-level paging) and far from
     * small integers, so a handle integer can never be mistaken for an fd, a length, an errno, or a real
     * address returned by mmap (which the backend registers as foreign memory at its true address).
     */
    private static final long BASE = 1L << 60;

    private final Map<Addressable, Long> bases = new IdentityHashMap<>();
    private final TreeMap<Long, Addressable> byBase = new TreeMap<>();
    private final Map<Addressable, Long> sizes = new IdentityHashMap<>();
    private long next = BASE;

    /** {@code uintptr(unsafe.Pointer(p))}; nil is 0. {@code size} is the object's size in gc bytes. */
    public synchronized long uintptr(Addr p, long size) {
        if (p == null) {
            return 0;
        }
        Long base = bases.get(p.base());
        if (base == null) {
            base = next;
            next += (Math.max(size, 1) + 15) & ~15L;
            bases.put(p.base(), base);
            byBase.put(base, p.base());
            sizes.put(p.base(), size);
        }
        return base + p.off();
    }

    /** {@code unsafe.Pointer(u)}; 0 is nil; anything outside a live object traps. One-past-the-end is allowed, as in Go. */
    public synchronized Addr pointer(long u) {
        if (u == 0) {
            return null;
        }
        Map.Entry<Long, Addressable> e = byBase.floorEntry(u);
        if (e == null || u - e.getKey() > sizes.get(e.getValue())) {
            throw new UnsafeTrap("uintptr 0x" + Long.toHexString(u) + " does not denote a live object");
        }
        return new Addr(e.getValue(), u - e.getKey());
    }
}
