package graalgo.p3unsafe;

import graalgo.values.GoCell;
import graalgo.values.GoPointer;
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
 *   <li>{@link #derive(long, long)} (fix-redmaster-r39): byte-offset
 *       derivation from a live handle, interned as a {@link ByteRef}
 *       view of the base's root referent. This is the only path by which
 *       {@code uintptr} arithmetic reaches a referent, and it is only
 *       reachable from frontend-proven derivations and
 *       {@code unsafe.Add} — a bare integer still cannot materialize a
 *       handle (see {@code UnsafeOps.fromUintTrap}).</li>
 * </ul>
 *
 * <p>Referent shapes the table holds: Java typed arrays (whole-array
 * backing), {@link ArrayRef} (a {@code SliceData} view: array + offset),
 * {@link graalgo.values.GoString} (a {@code StringData} view),
 * {@link graalgo.values.GoStruct}, {@link graalgo.values.GoMap},
 * {@link GoCell} (a cell pointer's storage: {@code new(T)} scalars and
 * {@code &x} — P3-M10), and any other object a {@code GoPointer} referent
 * can be. Handles keep their referent alive (strong refs) — this matches
 * gc's "a live {@code unsafe.Pointer} keeps the object alive", with the documented
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

    /**
     * A byte-offset view of another referent (fix-redmaster-r39): the
     * referent of a pointer derived by proven {@code uintptr} arithmetic
     * ({@code unsafe.Pointer(uintptr(p) + byteOffset)}) or by
     * {@code unsafe.Add}. The {@code base} is the root referent the
     * derivation started from (never itself a {@code ByteRef} —
     * {@link #derive} flattens), and {@code byteOffset} is the total byte
     * offset from the root's start. The view is only ever interpreted by
     * consumption sites that know the target element size
     * ({@code UnsafeToPtrNode}): the byte offset must divide evenly by
     * that size and land inside the root array, or the conversion traps.
     * A {@code ByteRef} therefore can never name storage outside its root
     * referent — the root is carried, never reconstructed from a number.
     *
     * <p>Equality: two views are equal iff they view the same root at the
     * same byte offset. Root equality is identity, except for
     * {@link ArrayRef} roots, whose own value equality (same array, same
     * element offset) applies.
     */
    public static final class ByteRef {
        private final Object base;
        private final long byteOffset;

        public ByteRef(Object base, long byteOffset) {
            if (base == null) {
                throw new IllegalArgumentException("ByteRef base must not be null");
            }
            if (base instanceof ByteRef) {
                throw new IllegalArgumentException("ByteRef base must be a root referent, not a ByteRef");
            }
            this.base = base;
            this.byteOffset = byteOffset;
        }

        /** The root referent this view is an offset into (never a ByteRef). */
        public Object base() {
            return base;
        }

        /** The total byte offset from the root's start. */
        public long byteOffset() {
            return byteOffset;
        }

        private static boolean baseEquals(Object a, Object b) {
            return a == b || (a instanceof ArrayRef && a.equals(b));
        }

        private static int baseHash(Object a) {
            return a instanceof ArrayRef ? a.hashCode() : System.identityHashCode(a);
        }

        @Override
        public boolean equals(Object o) {
            if (!(o instanceof ByteRef other)) {
                return false;
            }
            return byteOffset == other.byteOffset && baseEquals(base, other.base);
        }

        @Override
        public int hashCode() {
            return baseHash(base) * 31 + Long.hashCode(byteOffset);
        }

        @Override
        public String toString() {
            return "ByteRef{offset=" + byteOffset + ", base=" + base + "}";
        }
    }

    /** A resolved handle fully unwrapped: its root referent plus the total byte offset of any {@link ByteRef} layers. */
    public record ResolvedView(Object root, long byteOffset) {}

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
     * The handle for a {@code GoPointer} by storage identity (the
     * {@code unsafefromptr} rule, shared with {@link UnsafeFromPtrNode}
     * and with the interface-view builder in
     * {@code graalgo.p3reflect.AbiTypes}): a cell pointer interns its
     * cell, an element pointer interns an {@link ArrayRef}, a plain
     * pointer interns its referent, a nil pointer is handle {@code 0}.
     * Storage identity — never the dereferenced value — so distinct
     * allocations sharing a zero value keep distinct handles (P3-M10).
     */
    public static long handleForPointer(GoPointer gp) {
        if (gp == null || gp.isNil()) {
            return 0;
        }
        if (gp.isCellPointer()) {
            return current().intern(gp.cell());
        }
        if (gp.isElemPointer()) {
            return current().internEqual(new ArrayRef(gp.elemArray(), gp.elemIndex()));
        }
        return current().intern(gp.referent());
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
     * Resolves a handle and unwraps any {@link ByteRef} layers into a
     * {@link ResolvedView}: the root referent plus the total byte offset.
     * A handle that was never byte-derived yields offset {@code 0} and
     * its plain referent as the root.
     */
    public ResolvedView resolveView(long handle) {
        Object r = resolve(handle);
        long offset = 0;
        while (r instanceof ByteRef br) {
            offset += br.byteOffset();
            r = br.base();
        }
        return new ResolvedView(r, offset);
    }

    /**
     * Derives a pointer from a live base handle by a byte offset
     * (fix-redmaster-r39): the runtime half of the {@code "unsafederive"}
     * wire node and of {@code unsafe.Add}. The base is a handle that is
     * live <em>now</em> — provenance is carried by the handle itself,
     * never reconstructed from an integer — so the result can only ever
     * view the base's own referent:
     *
     * <ul>
     *   <li>offset {@code 0} → the base handle unchanged (an exact
     *       {@code unsafe.Pointer(uintptr(p))} round trip);</li>
     *   <li>a nil base (handle {@code 0}) with a nonzero offset traps —
     *       there is no referent to derive from;</li>
     *   <li>otherwise the derived view {@link ByteRef} {@code (root,
     *       totalOffset)} is interned by value equality, so the same
     *       derivation always yields the same handle. Whether the view
     *       names a valid element is decided at the consumption site,
     *       which knows the element size (see {@code UnsafeToPtrNode}).</li>
     * </ul>
     */
    public synchronized long derive(long baseHandle, long byteOffset) {
        if (byteOffset == 0) {
            return baseHandle;
        }
        Object r = resolve(baseHandle);
        if (r == null) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafe: byte derivation from a nil unsafe.Pointer traps "
                            + "(D5: no referent to derive from)");
        }
        long total = byteOffset;
        Object root = r;
        while (root instanceof ByteRef br) {
            total += br.byteOffset();
            root = br.base();
        }
        if (total == 0) {
            return root instanceof ArrayRef ref ? internEqual(ref) : intern(root);
        }
        return internEqual(new ByteRef(root, total));
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
