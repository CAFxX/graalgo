package graalgo.p3unsafe;

import graalgo.values.UnsafeHandles;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Map;

/**
 * The interned handle table backing Go's {@code unsafe.Pointer} (P3-M8).
 *
 * <p><b>FROZEN CONTRACT</b> (P3-M8 coordinator ruling): the unsafe worker
 * owns this API; the reflect and crypto workers build against it. Any
 * change needs coordinator approval. The contract:
 *
 * <ul>
 *   <li>{@link #install()} installs a fresh per-run table; every P3SyncMain
 *       run installs exactly once (same pattern as
 *       {@code LinknameIntrinsics.install}). {@link #current()} fails fast
 *       when no run is installed.</li>
 *   <li>{@link #intern(Object)}: identity interning — the same referent
 *       object always yields the same handle, so {@code p1 ==
 *       unsafe.Pointer(&x); p2 := unsafe.Pointer(&x); p1 == p2} is true
 *       (gc parity). {@code null} interns to handle {@code 0} (the nil
 *       {@code unsafe.Pointer}). Bare scalar boxes ({@code Integer},
 *       {@code Long}, {@code Float}, {@code Double}, {@code Boolean} —
 *       what {@code new(T)} produces for scalar {@code T}) are normalized
 *       to single-element carrier arrays <em>before</em> interning, so a
 *       table referent is never a bare scalar box: {@code unsafe.Slice}
 *       over a scalar pointer aliases correctly across calls.</li>
 *   <li>{@link #internEqual(Object)}: value-equality interning for key
 *       types with {@code equals}/{@code hashCode} — the reflect
 *       worker's {@code Ref} holders ({@code StructFieldRef},
 *       {@code ArrayElemRef}, …) and {@link ArrayRef} below.
 *       {@code null} → {@code 0}.</li>
 *   <li>{@link #resolve(long)}: handle → referent. {@code 0} → {@code null};
 *       unknown or freed handles throw
 *       {@link UnsafeHandles.UnsafeEscapeException} — never {@code null},
 *       never a stale referent.</li>
 *   <li>{@link #free(long)}: releases a handle; the numeric value is never
 *       reused (D5 use-after-free guarantee). The referent is dropped from
 *       the intern maps, so re-interning mints a fresh handle.</li>
 *   <li>{@code ==} semantics: {@code unsafe.Pointer} values are {@code long}
 *       handles ({@code GoKind.UNSAFE_POINTER}, D5); {@code ==} compares
 *       handle values. Interning is what makes this match gc.</li>
 * </ul>
 *
 * <p>Referent shapes the table holds in M8: Java typed arrays (whole-array
 * backing), {@link ArrayRef} (a {@code SliceData} view: array + offset),
 * {@link graalgo.values.GoString} (a {@code StringData} view),
 * {@link graalgo.values.GoStruct}, {@link graalgo.values.GoMap}, and any
 * other object a {@code GoPointer} referent can be. Handles keep their
 * referent alive (strong refs) — this matches gc's "a live
 * {@code unsafe.Pointer} keeps the object alive", with the documented
 * memory-retention caveat (R-US-2): churning {@code SliceData} in a loop
 * pins memory where gc would free.
 *
 * <p>All methods are thread-safe (the P3 scheduler runs goroutines on many
 * threads; handles cross goroutine boundaries freely).
 */
public final class UnsafeTable {

    /**
     * A {@code SliceData} view: the backing array plus the slice's offset
     * into it. Value type with identity-on-array equality: two views of the
     * same array at the same offset are the same pointer (gc:
     * {@code SliceData(s) == SliceData(s)}), while {@code SliceData(s)}
     * vs {@code SliceData(s[1:])} differ (gc compares addresses, and we
     * cannot fabricate an interior address — the offset rides in the key
     * instead).
     */
    public static final class ArrayRef {
        private final Object array;
        private final int offset;

        public ArrayRef(Object array, int offset) {
            if (array == null) {
                throw new IllegalArgumentException("ArrayRef array must not be null");
            }
            if (offset < 0) {
                throw new IllegalArgumentException("ArrayRef offset must not be negative");
            }
            this.array = array;
            this.offset = offset;
        }

        /** The backing Java array (never null). */
        public Object array() {
            return array;
        }

        /** The view's offset into the array. */
        public int offset() {
            return offset;
        }

        @Override
        public boolean equals(Object o) {
            if (!(o instanceof ArrayRef other)) {
                return false;
            }
            return array == other.array && offset == other.offset;
        }

        @Override
        public int hashCode() {
            return System.identityHashCode(array) * 31 + offset;
        }

        @Override
        public String toString() {
            return "ArrayRef{offset=" + offset + ", array=" + array.getClass().getSimpleName() + "}";
        }
    }

    private static volatile UnsafeTable current;

    /** Installs a fresh per-run table (called once per P3SyncMain run). */
    public static void install() {
        current = new UnsafeTable();
    }

    /** The current run's table; fails fast when no run is installed. */
    public static UnsafeTable current() {
        UnsafeTable t = current;
        if (t == null) {
            throw new IllegalStateException("UnsafeTable: no run installed (P3SyncMain must call install())");
        }
        return t;
    }

    private final UnsafeHandles handles = new UnsafeHandles();
    private final Map<Object, Long> interned = new IdentityHashMap<>();
    private final Map<Object, Long> internedEqual = new HashMap<>();
    // P1-3: box -> carrier cache for normalizeScalar idempotency. The same
    // box object must always normalize to the same carrier array, otherwise
    // IdentityHashMap interning mints a fresh handle per call (p1 != p2).
    private final Map<Object, Object> normalized = new IdentityHashMap<>();

    private UnsafeTable() {}

    /**
     * Identity interning: the same referent object always yields the same
     * handle. {@code null} → {@code 0}. Bare scalar boxes are normalized
     * to single-element carrier arrays first (see class javadoc).
     */
    public synchronized long intern(Object referent) {
        Object r = normalizeScalar(referent);
        if (r == null) {
            return 0;
        }
        Long h = interned.get(r);
        if (h == null) {
            h = handles.alloc(r);
            interned.put(r, h);
        }
        return h;
    }

    /**
     * Value-equality interning for key types defining
     * {@code equals}/{@code hashCode} (the reflect worker's {@code Ref}
     * holders; {@link ArrayRef}). {@code null} → {@code 0}.
     */
    public synchronized long internEqual(Object key) {
        if (key == null) {
            return 0;
        }
        Long h = internedEqual.get(key);
        if (h == null) {
            h = handles.alloc(key);
            internedEqual.put(key, h);
        }
        return h;
    }

    /**
     * Resolves a handle to its referent. {@code 0} → {@code null}; unknown
     * or freed handles throw {@link UnsafeHandles.UnsafeEscapeException}.
     */
    public Object resolve(long handle) {
        return handles.get(handle);
    }

    /**
     * Releases a handle; the numeric value is never reused. The referent
     * leaves the intern maps, so re-interning mints a fresh handle.
     */
    public synchronized void free(long handle) {
        if (handle == 0) {
            return;
        }
        Object referent = handles.get(handle); // throws on unknown/double-free
        interned.remove(referent);
        internedEqual.remove(referent);
        handles.free(handle);
    }

    /** Number of live handles (diagnostics). */
    public int liveCount() {
        return handles.liveCount();
    }

    /**
     * Wraps bare scalar boxes in single-element carrier arrays using the
     * box's own carrier ({@code Integer}→{@code int[1]},
     * {@code Long}→{@code long[1]}, {@code Float}→{@code float[1]},
     * {@code Double}→{@code double[1]}, {@code Boolean}→{@code boolean[1]}).
     * Everything else passes through unchanged.
     *
     * <p>P1-3: idempotent via the {@link #normalized} cache — the same box
     * object always yields the same carrier array, so identity interning
     * is stable (p1 == p2 for the same pointer).
     */
    Object normalizeScalar(Object referent) {
        if (referent instanceof Integer
                || referent instanceof Long
                || referent instanceof Float
                || referent instanceof Double
                || referent instanceof Boolean) {
            Object carrier = normalized.get(referent);
            if (carrier == null) {
                carrier = makeCarrier(referent);
                normalized.put(referent, carrier);
            }
            return carrier;
        }
        return referent;
    }

    private static Object makeCarrier(Object referent) {
        if (referent instanceof Integer i) {
            return new int[]{i.intValue()};
        }
        if (referent instanceof Long l) {
            return new long[]{l.longValue()};
        }
        if (referent instanceof Float f) {
            return new float[]{f.floatValue()};
        }
        if (referent instanceof Double d) {
            return new double[]{d.doubleValue()};
        }
        if (referent instanceof Boolean b) {
            return new boolean[]{b.booleanValue()};
        }
        return referent;
    }
}
