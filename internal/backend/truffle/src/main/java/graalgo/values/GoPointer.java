package graalgo.values;

/**
 * A Go pointer value (M4, WS E): an opaque reference to a referent object.
 *
 * <p>Per PLAN.md D5, pointers are opaque Java references; this holder is
 * the Phase-1 representation. A {@code null} referent is the nil pointer.
 * Pointers arise in M4 from {@code &T{...}} and from the implicit
 * {@code &T{}} element elision in composite literals (e.g.
 * {@code []*Point{{1, 2}}}); explicit {@code &x} / {@code *p} stay WS D.
 * Selectors auto-deref through non-nil pointers (nil → gc's nil-dereference
 * panic, M5 WS F).
 */
public final class GoPointer {
    private Object referent;
    // P3-M9 (WS D): element pointer for &arr[i]. When non-null, the pointer
    // refers to array[index]; referent() reads and setReferent() writes the
    // array slot (so *p = v updates arr[i], not a copy).
    private Object elemArray;
    private int elemIndex;
    private boolean isElemPtr;
    // P3-M9 (WS D): cell pointer for &x where x is a variable (or field)
    // whose storage is a GoCell. referent() reads the cell's current value
    // and setReferent() writes it, so *p = v is observable through the
    // variable and vice versa. A cell pointer is never nil.
    private final GoCell cell;

    public GoPointer(Object referent) {
        this.referent = referent;
        this.isElemPtr = false;
        this.cell = null;
    }

    /**
     * fix-redmaster-r43: storage unwrap hook, registered by the
     * reflect runtime (interop) for storage wrappers the values
     * package cannot name (reflect.New cells). The frontend's
     * offset-0 descent consults it to reach the value a cell
     * holds; unset (pure-frontend tests) it is the identity.
     * Set once at class-load of the wrapper's class; never cleared.
     */
    public static java.util.function.Function<Object, Object> storageUnwrapHook;

    /** Applies {@link #storageUnwrapHook} if set, else returns o. */
    public static Object unwrapStorage(Object o) {
        return storageUnwrapHook == null ? o : storageUnwrapHook.apply(o);
    }

    /**
     * fix-redmaster-r43: handle→referent resolve hook, registered
     * by the unsafe runtime (interop) for intern handles the values
     * package cannot resolve. The frontend's word-store branch
     * consults it to land a handle-valued unsafe.Pointer store in
     * a *T field. Unset (or unresolvable) it yields null and the
     * caller falls back to its previous behavior. Set once at
     * class-load of the unsafe table; never cleared.
     */
    public static java.util.function.Function<Long, Object> handleResolveHook;

    /** Resolves an intern handle via {@link #handleResolveHook}, or null. */
    public static Object resolveHandle(long handle) {
        if (handleResolveHook == null) {
            return null;
        }
        try {
            return handleResolveHook.apply(handle);
        } catch (RuntimeException e) {
            return null;
        }
    }

    /**
     * fix-redmaster-stdlibgate: address word for a pointer value, for
     * word consumers ({@code Boxing.toLong}, unsafe.Pointer word reads).
     * The values package cannot name the unsafe table, so the table
     * registers {@link #handleForPointerHook} at class-load (same pattern
     * as {@link #handleResolveHook}); the hook yields the canonical
     * intern handle for the pointer's storage (cell/elem/referent, 0 for
     * nil). The fallback (hook unset, e.g. unit contexts without the
     * runtime) synthesizes a stable non-zero word from
     * {@link #storageHash}, which is consistent per storage identity.
     */
    public static java.util.function.Function<GoPointer, Long> handleForPointerHook;

    /** The address word denoting this pointer (0 for nil). */
    public static long handleForPointer(GoPointer gp) {
        if (gp == null || gp.isNil()) {
            return 0;
        }
        if (handleForPointerHook != null) {
            try {
                Long h = handleForPointerHook.apply(gp);
                if (h != null) {
                    return h.longValue();
                }
            } catch (RuntimeException e) {
                // Fall through to the storage-hash word below.
            }
        }
        return (gp.storageHash() & 0xFFFFFFFFL) | 1L;
    }

    /**
     * The {@code *T} pointer value denoted by an unsafe address word, for
     * stores into pointer-typed storage (a {@link GoBox} with kind
     * {@code POINTER}, a {@code POINTER} struct field, a {@code POINTER}
     * array element). gc memory is flat: a word store through an
     * {@code unsafe.Pointer} view lands the denoted pointer in the slot —
     * real reflect's {@code Set} else-branch
     * ({@code *(*unsafe.Pointer)(v.ptr) = x.ptr}) stores exactly such a
     * word, and for a direct-iface zero value (real {@code Zero} returns
     * ptr nil without flagIndir) that word is 0. Address 0 lands nil; a
     * resolvable address lands a pointer to what it denotes (a cell
     * pointer for cell storage, else a plain pointer — mirroring
     * {@code DerefWriteNode}'s handle-valued {@code *T} field store). An
     * unresolvable address returns {@code fallback} (the caller keeps
     * today's behavior rather than inventing a value).
     */
    public static Object pointerForAddress(long handle, Object fallback) {
        if (handle == 0) {
            return null;
        }
        Object resolved = resolveHandle(handle);
        if (resolved instanceof GoPointer gp) {
            return gp;
        }
        if (resolved instanceof GoCell gc) {
            return new GoPointer(gc);
        }
        if (resolved != null) {
            return new GoPointer(resolved);
        }
        return fallback;
    }

    /** Creates a pointer to the storage behind a {@link GoCell} (&x, &s.f). */
    public GoPointer(GoCell cell) {
        this.cell = cell;
        this.isElemPtr = false;
        this.referent = null; // not used for cell pointers
    }

    /**
     * Creates a pointer to an array/slice element (P3-M9, WS D: {@code &arr[i]}).
     * The array is the Java array object (Object[] or primitive array);
     * index is the element index.
     */
    public GoPointer(Object array, int index) {
        this.elemArray = array;
        this.elemIndex = index;
        this.isElemPtr = true;
        this.referent = null; // not used for element pointers
        this.cell = null;
    }

    /** The pointed-to object, or {@code null} for the nil pointer. */
    public Object referent() {
        if (cell != null) {
            return cell.get();
        }
        if (isElemPtr) {
            return java.lang.reflect.Array.get(elemArray, elemIndex);
        }
        return referent;
    }

    /**
     * Sets the pointed-to object (P3-M9, WS D: {@code *p = v} assignment).
     * The pointer itself is the mutable cell; this is what makes
     * dereference writes observable through the pointer. For element
     * pointers, writes go to the array slot; for cell pointers, to the
     * variable/field storage the cell addresses.
     */
    public void setReferent(Object referent) {
        if (cell != null) {
            cell.set(referent);
            return;
        }
        if (isElemPtr) {
            java.lang.reflect.Array.set(elemArray, elemIndex, referent);
            return;
        }
        this.referent = referent;
    }

    public boolean isNil() {
        if (cell != null) {
            return false;
        }
        if (isElemPtr) {
            return elemArray == null;
        }
        return referent == null;
    }

    /**
     * True iff this is a cell pointer ({@code &x}, {@code &s.f},
     * {@code new(T)} scalars): the storage identity is the cell itself.
     * (P3-M10: {@code unsafe.Pointer} interning keys on storage identity,
     * not on the dereferenced value — see {@code UnsafeFromPtrNode}.)
     */
    public boolean isCellPointer() {
        return cell != null;
    }

    /** The cell behind a cell pointer; null unless {@link #isCellPointer}. */
    public GoCell cell() {
        return cell;
    }

    /**
     * Canonical key for storage-identity hashing (fix-redmaster-r43):
     * two pointers to the same storage yield equal keys — cell
     * pointers via the cell's storage key, element pointers via
     * (array, index), plain pointers via the referent object (whose
     * identity IS the storage for shared referents) or, for a plain
     * pointer to a cell, via that cell's storage key. Callers guard
     * nil (null referent) before calling.
     */
    public Object storageKey() {
        if (cell != null) {
            return cell.storageKey();
        }
        if (isElemPtr) {
            return new GoCell.StorageKey(elemArray, elemIndex);
        }
        if (referent instanceof GoCell gc) {
            return gc.storageKey();
        }
        return referent;
    }

    /** True iff this is an {@code &arr[i]} element pointer. */
    public boolean isElemPointer() {
        return isElemPtr;
    }

    /** The array of an element pointer; undefined unless {@link #isElemPointer}. */
    public Object elemArray() {
        return elemArray;
    }

    /** The index of an element pointer; undefined unless {@link #isElemPointer}. */
    public int elemIndex() {
        return elemIndex;
    }

    /**
     * Hash consistent with {@link #sameStorageAs}, for pointer map keys
     * and interface comparisons via {@link GoEquality}: nil is 0, cells
     * hash by their storage identity, array elements by (array, index),
     * plain pointers by their referent's identity.
     */
    public int storageHash() {
        if (isNil()) {
            return 0;
        }
        if (cell != null) {
            return cell.storageHash();
        }
        if (isElemPtr) {
            return 31 * System.identityHashCode(elemArray) + elemIndex;
        }
        return System.identityHashCode(referent);
    }

    /**
     * Pointer equality (Go {@code ==} on pointers): two pointers are equal
     * iff both are nil or they address the same storage — the same cell,
     * the same array element, or the same referent object. (Comparing
     * referent <em>values</em> would conflate distinct variables holding
     * equal values.)
     */
    public boolean sameStorageAs(GoPointer o) {
        if (isNil() || o.isNil()) {
            return isNil() && o.isNil();
        }
        if (cell != null || o.cell != null) {
            // PKG-011: cell identity is storage identity, not object
            // identity — e.g. two evaluations of &s.f allocate distinct
            // GoFieldCell objects over the same (struct, field). A cell
            // pointer never addresses the same storage as a non-cell
            // pointer (heap referent or array element).
            return cell != null && o.cell != null && cell.sameStorageAs(o.cell);
        }
        if (isElemPtr || o.isElemPtr) {
            return isElemPtr && o.isElemPtr
                    && elemArray == o.elemArray && elemIndex == o.elemIndex;
        }
        return referent == o.referent;
    }

    @Override
    public String toString() {
        if (cell != null) {
            return "GoPointer{cell " + cell + "}";
        }
        if (isElemPtr) {
            return "GoPointer{&arr[" + elemIndex + "]}";
        }
        return isNil() ? "GoPointer{nil}" : "GoPointer{" + referent + "}";
    }
}
