package graalgo.p3reflect;

import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;

/**
 * Reference keys and handle helpers for the reflect package (P3-M8, wave 2).
 *
 * <p>A reflect {@code Value} is {@code {typ, ptr, flag}} where {@code ptr}
 * is a {@code long} handle into the frozen {@link UnsafeTable} contract
 * (owned by the unsafe worker — this package builds against it, never
 * duplicates it):
 * <ul>
 *   <li>Reference referents (the D5 value itself: {@link GoStruct},
 *       arrays, {@link GoSlice}, {@link GoPointer}, …) intern by identity
 *       via {@link UnsafeTable#intern} — the same object always yields the
 *       same handle (gc's address-identity parity).</li>
 *   <li>Derived references (struct fields, array/slice elements, cells)
 *       are immutable key objects interned by value via
 *       {@link UnsafeTable#internEqual} — the same (base, index) always
 *       yields the same handle, and the key resolves back to the referent
 *       on demand.</li>
 *   <li>Bare scalar boxes are normalized by the table itself
 *       (single-element carrier arrays), so {@code ValueOf(42)} twice mints
 *       two handles (gc's copy semantics for unaddressable scalars).</li>
 * </ul>
 *
 * <p>Resolution ({@link #resolve}) turns a handle back into the live
 * referent; unknown handles throw the table's
 * {@code UnsafeEscapeException} (never null, never stale).
 */
public final class ReflectRefs {
    private ReflectRefs() {}

    /** Mutable cell backing {@code reflect.New} results. */
    public static final class Cell {
        /** The current D5 value (canonical boxed form). */
        public Object value;

        public Cell(Object value) {
            this.value = value;
        }
    }

    /** Base key type for derived references. */
    public abstract static class Ref {
        /** The referent this reference denotes (resolved live). */
        public abstract Object referent();
    }

    /** A struct field: (base handle, field index). */
    public static final class FieldRef extends Ref {
        private final long base;
        private final int field;

        public FieldRef(long base, int field) {
            this.base = base;
            this.field = field;
        }

        @Override
        public Object referent() {
            Object b = resolve(base);
            if (b instanceof Cell c) {
                b = c.value;
            }
            if (!(b instanceof GoStruct s)) {
                throw new IllegalStateException(
                        "reflect: field reference base is not a struct");
            }
            return s;
        }

        /** The field index. */
        public int field() {
            return field;
        }

        /** The base handle (denotes the containing struct). */
        public long base() {
            return base;
        }

        @Override
        public boolean equals(Object o) {
            return o instanceof FieldRef other && base == other.base && field == other.field;
        }

        @Override
        public int hashCode() {
            return Long.hashCode(base) * 31 + field;
        }
    }

    /** An array element: (base handle, index). */
    public static final class ArrayElemRef extends Ref {
        private final long base;
        private final int index;

        public ArrayElemRef(long base, int index) {
            this.base = base;
            this.index = index;
        }

        @Override
        public Object referent() {
            Object b = resolve(base);
            if (b instanceof Cell c) {
                b = c.value;
            }
            if (b == null || !b.getClass().isArray()) {
                throw new IllegalStateException(
                        "reflect: array element reference base is not an array");
            }
            return b;
        }

        public int index() {
            return index;
        }

        /** The base handle (denotes the containing array). */
        public long base() {
            return base;
        }

        @Override
        public boolean equals(Object o) {
            return o instanceof ArrayElemRef other && base == other.base && index == other.index;
        }

        @Override
        public int hashCode() {
            return Long.hashCode(base) * 31 + index;
        }
    }

    /** A slice element: (base handle, index). */
    public static final class SliceElemRef extends Ref {
        private final long base;
        private final int index;

        public SliceElemRef(long base, int index) {
            this.base = base;
            this.index = index;
        }

        @Override
        public Object referent() {
            Object b = resolve(base);
            if (b instanceof Cell c) {
                b = c.value;
            }
            if (!(b instanceof GoSlice s)) {
                throw new IllegalStateException(
                        "reflect: slice element reference base is not a slice");
            }
            return s;
        }

        public int index() {
            return index;
        }

        /** The base handle (denotes the containing slice). */
        public long base() {
            return base;
        }

        @Override
        public boolean equals(Object o) {
            return o instanceof SliceElemRef other && base == other.base && index == other.index;
        }

        @Override
        public int hashCode() {
            return Long.hashCode(base) * 31 + index;
        }
    }

    // ------------------------------------------------------------------
    // Handle helpers.
    // ------------------------------------------------------------------

    /** Interns a referent object by identity (null → 0). */
    public static long intern(Object referent) {
        return UnsafeTable.current().intern(referent);
    }

    /** Interns a derived-reference key by value (null → 0). */
    public static long internRef(Ref ref) {
        return UnsafeTable.current().internEqual(ref);
    }

    /** Resolves a handle to its referent (unknown → UnsafeEscapeException). */
    public static Object resolve(long handle) {
        return UnsafeTable.current().resolve(handle);
    }

    /**
     * Unwraps a referent to the D5 value it denotes: {@link Cell}s open to
     * their current value; single-element carrier arrays (the table's
     * scalar normalization) open to element 0. Everything else passes
     * through.
     */
    public static Object deref(Object referent) {
        if (referent instanceof Cell c) {
            return c.value;
        }
        if (referent instanceof long[] a && a.length == 1) {
            return a[0];
        }
        if (referent instanceof int[] a && a.length == 1) {
            return a[0];
        }
        if (referent instanceof double[] a && a.length == 1) {
            return a[0];
        }
        if (referent instanceof float[] a && a.length == 1) {
            return a[0];
        }
        if (referent instanceof boolean[] a && a.length == 1) {
            return a[0];
        }
        return referent;
    }

    /** deref(resolve(handle)). */
    public static Object load(long handle) {
        return deref(resolve(handle));
    }

    /**
     * Resolves a handle to the D5 value it denotes, following derived
     * references (struct fields, array/slice elements) recursively to the
     * live referent. {@code kind} is the GoKind of the denoted value
     * (taken from the Value's type token); it is only consumed when the
     * leaf itself is primitive-carried, so intermediate containers pass
     * their fixed kinds ({@code STRUCT}/{@code ARRAY}/{@code SLICE}).
     *
     * <p>Unlike {@link #load}, this never surfaces a {@link Ref} key
     * object: a {@code Value} obtained via {@code Field} or {@code Index}
     * reads and writes through to the denoted field/element.
     */
    public static Object denotedValue(long handle, GoKind kind) {
        Object ref = resolve(handle);
        if (ref instanceof Cell c) {
            return deref(c.value);
        }
        if (ref instanceof FieldRef fr) {
            GoStruct s = (GoStruct) denotedValue(fr.base(), GoKind.STRUCT);
            return ReflectValues.structFieldGet(s, fr.field(), kind);
        }
        if (ref instanceof ArrayElemRef ar) {
            Object arr = denotedValue(ar.base(), GoKind.ARRAY);
            return ReflectValues.arrayGet(arr, kind, ar.index());
        }
        if (ref instanceof SliceElemRef sr) {
            GoSlice slice = (GoSlice) denotedValue(sr.base(), GoKind.SLICE);
            return ReflectValues.arrayGet(slice.array(), kind, slice.offset() + sr.index());
        }
        return deref(ref);
    }

    /**
     * True when the referent is a mutable cell (a {@link Cell} or a
     * mutable aggregate): stores through this handle are meaningful.
     */
    public static boolean isCellHandle(long handle) {
        return resolve(handle) instanceof Cell;
    }
}
