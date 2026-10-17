package graalgo.values;

import java.util.HashMap;
import java.util.Map;

/**
 * Handle table backing Go's {@code unsafe.Pointer} (D5).
 *
 * <p>An {@code unsafe.Pointer} value is an opaque handle ({@code long}) into
 * this table, never a raw address: there is no observable address anywhere
 * (GG-0015). The handle doubles as the {@code unsafe.Pointer → uintptr}
 * identity token (PKG-007): {@link GoConvert#unsafePointerToUintptr}
 * returns the handle itself, so the portable identity properties (nil → 0,
 * stability, distinctness, equality-as-identity) hold without exposing an
 * address. Arithmetic on handles and materializing a handle from an
 * integer ({@code uintptr → unsafe.Pointer}) <em>trap</em> — a handle can
 * never be fabricated from an integer, so the escaped token has no path
 * back to a referent. Handles are never reused after {@link #free}
 * (use-after-free fails loud, never silently aliases a new referent).
 *
 * <p>RV-P1-23: the table hosts two handle domains. <b>Uncounted</b> entries
 * (today's semantics) are anchored strongly for the run's lifetime — a bare
 * {@code long} {@code unsafe.Pointer} has undecidable liveness, so the
 * strong anchor <em>is</em> the gc-parity mechanism. <b>Counted</b> entries
 * back {@code reflect.Value} handles: each live {@code Value} (or copy)
 * holding the handle is a registered owner, and the entry is removed when
 * the last owner is released (driven by {@code java.lang.ref.Cleaner} on
 * the owning {@code GoStruct}s; see {@code UnsafeTable} in p3unsafe).
 */
public final class UnsafeHandles {
    /** Thrown when unsafe.Pointer arithmetic or an escaping conversion traps. */
    public static final class UnsafeEscapeException extends RuntimeException {
        public UnsafeEscapeException(String message) {
            super(message);
        }
    }

    /**
     * One handle-table entry: the strong referent, its domain, and (counted
     * domain only) the live-owner count.
     */
    public static final class Entry {
        /** The referent, held strongly while the entry lives. */
        public final Object referent;
        /** True for the counted (reflect.Value) domain. */
        public final boolean counted;

        private int owners;

        Entry(Object referent, boolean counted) {
            this.referent = referent;
            this.counted = counted;
        }
    }

    private final Map<Long, Entry> table = new HashMap<>();
    private long next = 1; // 0 is the nil unsafe.Pointer

    /** Interns {@code referent}, returning its handle. Null → 0 (nil). */
    public synchronized long alloc(Object referent) {
        if (referent == null) {
            return 0;
        }
        long h = next++;
        table.put(h, new Entry(referent, false));
        return h;
    }

    /**
     * Interns {@code referent} in the counted domain, returning its handle.
     * Null → 0 (nil). The owner count starts at 0: the caller must
     * {@link #addCountedOwner} before the handle is published, so the entry
     * can never be released under a live owner.
     */
    public synchronized long allocCounted(Object referent) {
        if (referent == null) {
            return 0;
        }
        long h = next++;
        table.put(h, new Entry(referent, true));
        return h;
    }

    /**
     * Registers one live owner of a counted handle. Unknown handles and
     * uncounted entries throw (fail loud on domain violations).
     */
    public synchronized void addCountedOwner(long handle) {
        Entry e = table.get(handle);
        if (e == null) {
            throw new UnsafeEscapeException("addCountedOwner: unknown handle: " + handle);
        }
        if (!e.counted) {
            throw new UnsafeEscapeException("addCountedOwner: uncounted handle: " + handle);
        }
        e.owners++;
    }

    /**
     * Releases one counted owner. When the count reaches zero the entry is
     * removed and this returns {@code true}. A missing entry is a defensive
     * no-op ({@code false}); releasing an uncounted entry or an entry with
     * no owners throws (protocol violation).
     */
    public synchronized boolean releaseCounted(long handle) {
        Entry e = table.get(handle);
        if (e == null) {
            return false;
        }
        if (!e.counted) {
            throw new UnsafeEscapeException("releaseCounted: uncounted handle: " + handle);
        }
        if (e.owners <= 0) {
            throw new UnsafeEscapeException("releaseCounted: handle with no owners: " + handle);
        }
        e.owners--;
        if (e.owners == 0) {
            table.remove(handle);
            return true;
        }
        return false;
    }

    /**
     * Resolves a handle. Unknown or freed handles throw — never null, never
     * a stale referent.
     */
    public synchronized Object get(long handle) {
        if (handle == 0) {
            return null;
        }
        Entry e = table.get(handle);
        if (e == null) {
            throw new UnsafeEscapeException("invalid unsafe.Pointer handle: " + handle);
        }
        return e.referent;
    }

    /**
     * Returns the entry for {@code handle}, or {@code null} for handle 0 or
     * unknown handles. For {@code UnsafeTable}'s counted-closure
     * computation; the caller must hold the table monitor (all
     * {@code UnsafeTable} callers are synchronized methods, so the
     * table→handles lock order is preserved).
     */
    public synchronized Entry entryFor(long handle) {
        if (handle == 0) {
            return null;
        }
        return table.get(handle);
    }

    /**
     * Releases a handle; the handle value is never reused. Freeing a
     * counted handle with live owners throws (an explicit free must never
     * strand live owners); a counted handle with zero owners is removed
     * (transient-window cleanup).
     */
    public synchronized void free(long handle) {
        if (handle == 0) {
            return;
        }
        Entry e = table.get(handle);
        if (e == null) {
            throw new UnsafeEscapeException("double-free of unsafe.Pointer handle: " + handle);
        }
        if (e.counted && e.owners > 0) {
            throw new UnsafeEscapeException("free of counted handle with live owners: " + handle);
        }
        table.remove(handle);
    }

    public synchronized int liveCount() {
        return table.size();
    }

    /** Number of live counted-domain handles (diagnostics). */
    public synchronized int liveCounted() {
        int n = 0;
        for (Entry e : table.values()) {
            if (e.counted) {
                n++;
            }
        }
        return n;
    }
}
