package graalgo.p3unsafe;

import graalgo.values.GoCell;
import graalgo.values.GoPointer;
import graalgo.values.UnsafeHandles;
import java.lang.ref.Cleaner;
import java.lang.ref.WeakReference;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.HashMap;
import java.util.HashSet;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.WeakHashMap;

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
 *       {@code unsafe.Pointer}). P3-M10: the interning key is the
 *       pointer's <em>storage identity</em> (the {@link GoCell} for cell
 *       pointers, the {@link ArrayRef} for element pointers — see
 *       {@link UnsafeFromPtrNode}), never a dereferenced value: distinct
 *       allocations always mint distinct handles, and a pointer keeps its
 *       handle across {@code *p = v} writes. Bare scalar boxes
 *       ({@code Integer}, {@code Long}, {@code Float}, {@code Double},
 *       {@code Boolean}) reaching {@code intern} directly are normalized
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
 * <p>RV-P1-23: the table hosts two handle domains (one table, one handle
 * sequence — {@code uintptr(handle)} tokens don't move):
 * <ul>
 *   <li><b>Uncounted (strong)</b> — {@link #intern}/{@link #internEqual} keep
 *       today's exact semantics, run lifetime. A {@code unsafe.Pointer} is
 *       just a {@code long} whose Go-liveness is undecidable, so the strong
 *       anchor <em>is</em> the gc-parity mechanism ("a live
 *       {@code unsafe.Pointer} keeps the object alive"). The documented
 *       retention caveat (R-US-2) now applies precisely to this domain.</li>
 *   <li><b>Counted</b> — {@link #internCounted}/{@link #internEqualCounted}
 *       back {@code reflect.Value} handles. A Value's handle is never
 *       Go-visible except inside the Value ({@code ptr} is unexported), so
 *       the entry lives exactly as long as the set of live Values (and
 *       copies) holding it: each is a registered owner, and the entry is
 *       removed when the last owner is released (driven by
 *       {@code java.lang.ref.Cleaner} on the owning {@code GoStruct}s, plus
 *       a copy hook on the Value descriptor — see p3reflect's
 *       {@code ReflectRefs}).</li>
 * </ul>
 *
 * <p>Referent shapes the table holds: Java typed arrays (whole-array
 * backing), {@link ArrayRef} (a {@code SliceData} view: array + offset),
 * {@link graalgo.values.GoString} (a {@code StringData} view),
 * {@link graalgo.values.GoStruct}, {@link graalgo.values.GoMap},
 * {@link GoCell} (a cell pointer's storage: {@code new(T)} scalars and
 * {@code &x} — P3-M10), and any other object a {@code GoPointer} referent
 * can be.
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

    /**
     * A map with weak keys compared by identity (like
     * {@link IdentityHashMap}, but entries evaporate when the key is no
     * longer strongly reachable). {@link WeakHashMap} compares by
     * {@code equals()}, which would conflate distinct scalar boxes holding
     * equal values — breaking the box→carrier idempotency contract (P1-3)
     * and gc's copy semantics for unaddressable scalars. All use is
     * confined to synchronized methods (the table monitor), so
     * {@code WeakHashMap}'s lazy stale-entry expunge runs under the same
     * lock.
     */
    private static final class WeakIdentityMap<V> {
        private static final class Key extends WeakReference<Object> {
            private final int hash;

            Key(Object referent) {
                super(referent);
                hash = System.identityHashCode(referent);
            }

            @Override
            public int hashCode() {
                return hash;
            }

            @Override
            public boolean equals(Object o) {
                if (!(o instanceof Key other)) {
                    return false;
                }
                Object a = get();
                // Dead keys never compare equal (their entries are expunged).
                return a != null && a == other.get();
            }
        }

        private final WeakHashMap<Key, V> inner = new WeakHashMap<>();

        V get(Object key) {
            return inner.get(new Key(key));
        }

        V put(Object key, V value) {
            return inner.put(new Key(key), value);
        }

        V remove(Object key) {
            return inner.remove(new Key(key));
        }
    }

    private static volatile UnsafeTable current;

    /** The Cleaner driving counted-handle release (one per JVM). */
    private static final Cleaner CLEANER = Cleaner.create();

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
    // RV-P1-23: the counted domain's indexes, weak-keyed. Sound by
    // re-presentation: interning requires the referent live in the caller's
    // hand, so a weak entry can never evaporate between two interns of the
    // same object; the handle entries (not the indexes) are the anchor.
    // Explicit removal on release is the primary path (no ReferenceQueue).
    private final WeakIdentityMap<Long> countedInterned = new WeakIdentityMap<>();
    private final Map<Object, Long> countedInternedEqual = new WeakHashMap<>();
    // P1-3: box -> carrier cache for normalizeScalar idempotency. The same
    // box object must always normalize to the same carrier array, otherwise
    // IdentityHashMap interning mints a fresh handle per call (p1 != p2).
    // RV-P1-23: weak-keyed (identity semantics — distinct equal-valued
    // boxes keep distinct carriers). Sound by re-presentation: the box is
    // strongly reachable as normalizeScalar's argument, so its entry
    // cannot evaporate mid-call; the carrier stays anchored by the handle
    // entry while it lives.
    private final WeakIdentityMap<Object> normalized = new WeakIdentityMap<>();

    private UnsafeTable() {}

    /**
     * Identity interning: the same referent object always yields the same
     * handle. {@code null} → {@code 0}. Bare scalar boxes are normalized
     * to single-element carrier arrays first (see class javadoc).
     *
     * <p>Uncounted domain: the entry is anchored strongly for the run.
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
     *
     * <p>Uncounted domain: the entry is anchored strongly for the run.
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
     * Identity interning in the counted domain (RV-P1-23): the same
     * referent object yields the same handle while its entry lives, but
     * the entry is removed once its last owner is released (see
     * {@link #addCountedOwner}). Normalizes scalar boxes exactly like
     * {@link #intern}. {@code null} → {@code 0}.
     */
    public synchronized long internCounted(Object referent) {
        Object r = normalizeScalar(referent);
        if (r == null) {
            return 0;
        }
        Long h = countedInterned.get(r);
        if (h == null) {
            h = handles.allocCounted(r);
            countedInterned.put(r, h);
        }
        return h;
    }

    /**
     * Value-equality interning in the counted domain (RV-P1-23): for key
     * types with {@code equals}/{@code hashCode} (the reflect worker's
     * {@code Ref} holders). {@code null} → {@code 0}.
     */
    public synchronized long internEqualCounted(Object key) {
        if (key == null) {
            return 0;
        }
        Long h = countedInternedEqual.get(key);
        if (h == null) {
            h = handles.allocCounted(key);
            countedInternedEqual.put(key, h);
        }
        return h;
    }

    /**
     * The transitive closure of counted-domain handles denoted by
     * {@code handle}: the handle itself (when counted) plus every counted
     * handle reachable through {@link BaseHandleProvider} base links.
     * Derived {@code Ref} keys resolve their base lazily, so a child Value
     * must own the whole base chain (gc keeps the parent object alive
     * through the child's pointer). Uncounted handles in the chain are
     * skipped — the unsafe domain anchors them strongly for the run.
     * Cycle-safe (visited set); unknown handles throw. {@code 0} → empty.
     */
    public synchronized long[] countedClosure(long handle) {
        if (handle == 0) {
            return new long[0];
        }
        List<Long> out = new ArrayList<>();
        Set<Long> seen = new HashSet<>();
        Deque<Long> work = new ArrayDeque<>();
        work.add(handle);
        while (!work.isEmpty()) {
            long h = work.removeFirst();
            if (!seen.add(h)) {
                continue;
            }
            UnsafeHandles.Entry e = handles.entryFor(h);
            if (e == null) {
                throw new UnsafeHandles.UnsafeEscapeException(
                        "countedClosure: unknown handle: " + h);
            }
            if (e.counted) {
                out.add(h);
            }
            if (e.referent instanceof BaseHandleProvider p) {
                for (long b : p.baseHandles()) {
                    if (b != 0) {
                        work.add(b);
                    }
                }
            }
        }
        long[] res = new long[out.size()];
        for (int i = 0; i < out.size(); i++) {
            res[i] = out.get(i);
        }
        return res;
    }

    /**
     * Registers one live counted owner of {@code handle} (fail loud on
     * unknown handles or domain violations — see
     * {@link UnsafeHandles#addCountedOwner}).
     */
    public synchronized void addCountedOwner(long handle) {
        handles.addCountedOwner(handle);
    }

    /**
     * Releases one counted owner of {@code handle}; when the last owner
     * goes the entry is removed from the table and from the counted
     * indexes. Unknown handles are a defensive no-op.
     */
    public synchronized void releaseCounted(long handle) {
        if (handle == 0) {
            return;
        }
        UnsafeHandles.Entry e = handles.entryFor(handle);
        if (e == null) {
            return;
        }
        if (handles.releaseCounted(handle)) {
            countedInterned.remove(e.referent);
            countedInternedEqual.remove(e.referent);
        }
    }

    /**
     * Registers a {@link Cleaner} action releasing {@code countedHandles}
     * when {@code owner} becomes phantom-reachable. The action captures
     * this table instance — never {@link #current()} — so a later
     * {@link #install()} cannot divert releases belonging to this run's
     * table. Empty handle sets (uncounted/shared handles) register nothing.
     * The action must not capture {@code owner} (else it would never
     * become phantom-reachable).
     */
    public void registerCountedRelease(Object owner, long[] countedHandles) {
        if (countedHandles.length == 0) {
            return;
        }
        CLEANER.register(owner, () -> {
            for (long h : countedHandles) {
                releaseCounted(h);
            }
        });
    }

    /**
     * Wraps a resolved handle referent as the Go {@code *T} pointer the
     * unsafe builtins traffic in — the inverse of the storage-identity
     * interning in {@link UnsafeFromPtrNode}: {@code resolve(handle)} feeds
     * this, and re-interning the result yields the same handle.
     *
     * <ul>
     *   <li>a {@link GoCell} (cell-pointer handle: {@code new(T)} scalars,
     *       {@code &x}) → a cell pointer, so {@code *p} reads and
     *       {@code *p = v} writes the cell (gc aliasing);</li>
     *   <li>an {@link ArrayRef} (element-pointer handle: {@code &arr[i]},
     *       {@code SliceData} views) → an element pointer, so reads and
     *       writes hit the array slot;</li>
     *   <li>anything else (plain-referent handles, {@code null} for handle
     *       0) → a plain {@code GoPointer} over the referent.</li>
     * </ul>
     */
    public static GoPointer pointerFor(Object referent) {
        if (referent instanceof GoCell cell) {
            return new GoPointer(cell);
        }
        if (referent instanceof ArrayRef ref) {
            return new GoPointer(ref.array(), ref.offset());
        }
        return new GoPointer(referent);
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
     * Freeing a counted handle with live owners throws (an explicit free
     * must never strand live owners).
     */
    public synchronized void free(long handle) {
        if (handle == 0) {
            return;
        }
        Object referent = handles.get(handle); // throws on unknown/double-free
        interned.remove(referent);
        internedEqual.remove(referent);
        countedInterned.remove(referent);
        countedInternedEqual.remove(referent);
        handles.free(handle);
    }

    /** Number of live handles (diagnostics). */
    public int liveCount() {
        return handles.liveCount();
    }

    /** Number of live counted-domain handles (diagnostics). */
    public int liveCounted() {
        return handles.liveCounted();
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
