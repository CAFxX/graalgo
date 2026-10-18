package graalgo.values;

/**
 * Storage identity of a {@link GoPointer}, as an immutable map key.
 *
 * <p>R15-P1-3: the finalizer registry ({@code graalgo.runtime.GcInterop})
 * must key registrations by the <em>storage</em> a pointer addresses, not
 * by the wrapper object — every {@code &x} evaluation mints a fresh
 * {@code GoPointer} wrapper over the same storage, so wrapper identity
 * misses double-sets. This key's {@code equals}/{@code hashCode} mirror
 * {@link GoPointer#sameStorageAs} <em>exactly</em>:
 * {@code of(a).equals(of(b))} iff {@code a.sameStorageAs(b)}.
 *
 * <p>The key is kind-tagged ({@code CELL}, {@code ELEM}, {@code REF},
 * {@code NIL}) to reproduce {@code sameStorageAs}'s cross-kind partition:
 * a cell pointer never equals an element or referent pointer, and an
 * element pointer only equals an element pointer. All nil pointers (of any
 * shape) share the single {@code NIL} key, matching {@code sameStorageAs}'s
 * {@code nil == nil} rule.
 *
 * <p>Structural, not name-keyed: the kind comes from {@code GoPointer}'s own
 * shape accessors ({@code isCellPointer}/{@code isElemPointer}), and cell
 * hashing/equality delegate to the polymorphic
 * {@link GoCell#storageHash}/{@link GoCell#sameStorageAs} — no
 * {@code instanceof} dispatch over cell types anywhere in this class. The
 * {@code ELEM} scheme (identity hash of the array combined with the index)
 * follows the precedent of the interop module's {@code UnsafeTable.ArrayRef}.
 *
 * <p>Instances are immutable and safe to share; the hash is computed once at
 * construction.
 */
public final class GoStorageKey {
    /** Storage-identity partition, mirroring GoPointer's pointer shapes. */
    private enum Kind {
        /** Cell pointer ({@code &x}, {@code &s.f}, {@code new(T)} scalar). */
        CELL,
        /** Array/slice element pointer ({@code &a[i]}). */
        ELEM,
        /** Heap-referent pointer ({@code &T{}}, {@code new(T)} non-scalar). */
        REF,
        /** Nil pointer, any shape ({@code sameStorageAs} equates all nils). */
        NIL,
    }

    private static final GoStorageKey NIL = new GoStorageKey();

    private final Kind kind;
    private final int hash;
    // Identity components; only the kind's own fields are significant.
    private final GoCell cell;
    private final Object array;
    private final int index;
    private final Object referent;

    /** The shared key for every nil pointer. */
    private GoStorageKey() {
        this.kind = Kind.NIL;
        this.hash = 0;
        this.cell = null;
        this.array = null;
        this.index = 0;
        this.referent = null;
    }

    private GoStorageKey(Kind kind, int hash, GoCell cell, Object array, int index,
            Object referent) {
        this.kind = kind;
        this.hash = hash;
        this.cell = cell;
        this.array = array;
        this.index = index;
        this.referent = referent;
    }

    /**
     * The storage-identity key for {@code p}. Two pointers addressing the
     * same Go storage — even via distinct wrapper objects, e.g. two
     * evaluations of {@code &x} — yield equal keys with equal hashes.
     */
    public static GoStorageKey of(GoPointer p) {
        if (p.isNil()) {
            return NIL;
        }
        if (p.isCellPointer()) {
            GoCell cell = p.cell();
            return new GoStorageKey(Kind.CELL, cell.storageHash(), cell, null, 0, null);
        }
        if (p.isElemPointer()) {
            Object array = p.elemArray();
            int index = p.elemIndex();
            return new GoStorageKey(Kind.ELEM, System.identityHashCode(array) * 31 + index,
                    null, array, index, null);
        }
        Object referent = p.referent();
        return new GoStorageKey(Kind.REF, System.identityHashCode(referent),
                null, null, 0, referent);
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) {
            return true;
        }
        if (!(o instanceof GoStorageKey k)) {
            return false;
        }
        if (kind != k.kind) {
            return false;
        }
        return switch (kind) {
            case NIL -> true;
            // Polymorphic cell comparison: no instanceof over cell types.
            case CELL -> cell.sameStorageAs(k.cell);
            case ELEM -> array == k.array && index == k.index;
            case REF -> referent == k.referent;
        };
    }

    @Override
    public int hashCode() {
        return hash;
    }

    @Override
    public String toString() {
        return "GoStorageKey{" + kind + " #" + Integer.toHexString(hash) + "}";
    }
}
