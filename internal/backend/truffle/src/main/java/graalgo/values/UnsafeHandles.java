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
 * address. Materializing a handle from an integer
 * ({@code uintptr → unsafe.Pointer}) <em>traps</em> — a handle can never
 * be fabricated from an integer, so the escaped token has no path back to
 * a referent. The single exception is not an exception to that rule:
 * derivations the frontend proves carry a live pointer's provenance
 * (fix-redmaster-r39) never pass through an integer at all — the
 * interop table derives a byte-offset view from the live base handle
 * itself ({@code UnsafeTable.derive}). Handles are never reused after
 * {@link #free} (use-after-free fails loud, never silently aliases a new
 * referent).
 */
public final class UnsafeHandles {
    /** Thrown when unsafe.Pointer arithmetic or an escaping conversion traps. */
    public static final class UnsafeEscapeException extends RuntimeException {
        public UnsafeEscapeException(String message) {
            super(message);
        }
    }

    private final Map<Long, Object> table = new HashMap<>();
    private long next = 1; // 0 is the nil unsafe.Pointer

    /** Interns {@code referent}, returning its handle. Null → 0 (nil). */
    public synchronized long alloc(Object referent) {
        if (referent == null) {
            return 0;
        }
        long h = next++;
        table.put(h, referent);
        return h;
    }

    /**
     * Resolves a handle. Unknown or freed handles throw — never null, never
     * a stale referent.
     */
    public synchronized Object get(long handle) {
        if (handle == 0) {
            return null;
        }
        Object referent = table.get(handle);
        if (referent == null) {
            throw new UnsafeEscapeException("invalid unsafe.Pointer handle: " + handle);
        }
        return referent;
    }

    /** Releases a handle; the handle value is never reused. */
    public synchronized void free(long handle) {
        if (handle != 0 && table.remove(handle) == null) {
            throw new UnsafeEscapeException("double-free of unsafe.Pointer handle: " + handle);
        }
    }

    public synchronized int liveCount() {
        return table.size();
    }
}
