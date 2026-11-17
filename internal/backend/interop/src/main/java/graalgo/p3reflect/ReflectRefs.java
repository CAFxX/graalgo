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

        static {
            // fix-redmaster-r43: let the values package (which
            // cannot name this class) unwrap New cells for the
            // frontend's offset-0 descent. This class loads before
            // any Cell exists, so the hook is always set in time.
            graalgo.values.GoPointer.storageUnwrapHook =
                    o -> o instanceof Cell c ? c.value : o;
        }

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
        // PKG-022: Value.Elem() on a pointer to a nil pointee interns the
        // pointee's GoCell storage (there is no value to intern). Unwrap it
        // here so denotedValue yields the live value (null for nil).
        if (referent instanceof graalgo.values.GoCell gc) {
            return gc.get();
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
        if (ref instanceof UnsafeTable.ByteRef) {
            // fix-redmaster-r59: a ByteRef into a struct denotes the
            // field at that byte offset for ANY kind (gc flat memory),
            // not just scalars — the maps iteration fast path reads
            // group.key(i)/group.elem(i) (struct, string, interface,
            // ...) through byte-derived pointers, and the old
            // scalar-only gate left those denoted as the ByteRef
            // itself. Non-struct roots keep the scalar-only array
            // walk below (a non-scalar kind over a non-struct root
            // denotes nothing readable and falls through to deref).
            UnsafeTable.ResolvedView view = UnsafeTable.current().resolveView(handle);
            Object root = view.root();
            if (root instanceof Cell c) {
                root = c.value;
            } else if (root instanceof graalgo.values.GoCell gc) {
                root = gc.get();
            }
            if (root instanceof GoStruct gs) {
                // A ByteRef into a struct denotes the field at
                // that byte offset (gc flat memory; reflect
                // Field reads): the array walk below cannot read
                // it and traps as a non-array root (snapshot
                // 11/20: main.Point + 8 = field Y).
                return readStructFieldAt(gs, view.byteOffset(), kind);
            }
            if (isScalarKind(kind)) {
                return readScalarAt(view.root(), view.byteOffset(), kind);
            }
            return deref(ref);
        }
        if (isScalarKind(kind)) {
            // fix-redmaster-r43: a handle denoting array storage read as
            // a scalar kind denotes the scalar at that address, not the
            // array object. This is the shape reflect's Index produces:
            // add(v.ptr, 0) collapses to the base handle in
            // UnsafeTable.derive, so packEface's interface data word
            // names array storage while the dynamic kind is scalar —
            // without this read, Interface() on an array element binds
            // the whole array (snapshot 36's r2 probe: [J as Long).
            // The read walks nested arrays level by level (a flat read
            // would cast Object[] to [J for [2][2]int — the regression
            // this walk replaces). Aggregate kinds and non-array roots
            // pass through unchanged (struct descent stays with
            // loadOffsetZero's own loop).
            if (ref instanceof UnsafeTable.ArrayRef ar) {
                return readScalarAtElement(ar.array(), ar.offset(), kind);
            }
            if (ref != null && ref.getClass().isArray()) {
                return readScalarAt(ref, 0, kind);
            }
        }
        return deref(ref);
    }

    /**
     * Reads the scalar of {@code kind} at {@code byteOffset} into an
     * array-like root (a Java array, an {@link UnsafeTable.ArrayRef}
     * element, or a {@link GoSlice} backing array; cells unwrap to
     * their value once). Walks nested arrays level by level: at each
     * Object[] level the byte offset is divided by that level's
     * element size (mirroring UnsafeToPtrNode's nested-array walk)
     * and the remainder descends. Primitive carriers read flat at
     * offset/element-size. Misaligned offsets, out-of-range indices,
     * empty arrays, and non-array roots fail loudly (D5: a derived
     * view that names no element traps rather than returning its
     * root).
     */
    /**
     * Reads the scalar of {@code kind} at {@code byteOffset} into a struct
     * value (gc flat memory: a pointer into a struct at a field offset
     * denotes that field; an offset strictly inside an aggregate field
     * descends into it with the remainder). Only direct fields are
     * matched (index path of length 1): guest Field walks promoted
     * paths field by field, so a ByteRef never names a promoted field
     * directly. Unnamed (dynamic) structs have no TypeInfo offset table
     * and offsets naming no field fail loudly — never a silent wrong
     * value.
     */
    private static Object readStructFieldAt(GoStruct gs, long byteOffset, GoKind kind) {
        ReflectTypes.TypeInfo info = ReflectTypes.infoForName(gs.descriptor().typeName());
        if (info == null) {
            // fix-redmaster-r59: unnamed structs (including the
            // synthesized group/slot structs, whose descriptor name is
            // the structural rendering) recover via typeString match
            // (the r43 precedent in UnsafeToPtrNode.structFieldAt).
            info = ReflectTypes.infoForStructString(gs.descriptor().typeName());
        }
        if (info == null) {
            throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                    "reflect: cannot read scalar at byte offset " + byteOffset
                            + " of unnamed struct " + gs.descriptor().typeName() + " (D5)");
        }
        for (ReflectTypes.FieldInfo f : info.fields) {
            if (f.type == null || f.index.length != 1) {
                continue;
            }
            if (byteOffset == f.offset) {
                // fix-redmaster-r59 (d): an exact field-boundary match
                // whose kind differs from the requested kind is NOT the
                // addressed storage when the field is a container:
                // offset 8 into a group image is the slots array, but a
                // STRING read there addresses slot 0's key — descend
                // into element 0 (array-of-struct) or field 0 (struct),
                // guarded by the descended field's kind, falling back
                // to the field read when the descent does not resolve.
                if (f.type.kind != null && f.type.kind != kind) {
                    Object descended = descendExactMatch(gs, f, kind);
                    if (descended != null) {
                        return descended;
                    }
                }
                return ReflectValues.structFieldGet(gs, f.index[0], kind);
            }
            if (f.type.size > 0 && byteOffset > f.offset && byteOffset < f.offset + f.type.size) {
                Object fv = ReflectValues.structFieldGet(gs, f.index[0], f.type.kind);
                if (fv instanceof GoStruct nested) {
                    return readStructFieldAt(nested, byteOffset - f.offset, kind);
                }
                // fix-redmaster-r59: an offset strictly inside an
                // array-of-struct field descends into the addressed
                // element (index = rel / elemSize, remainder recursed)
                // — gc flat memory, for the group image's slots array.
                // The generic array walk below cannot read struct
                // elements (it only descends nested arrays), so this
                // branch must come first.
                if (fv instanceof Object[] oa && f.type.elem != null
                        && f.type.elem.kind == GoKind.STRUCT && f.type.elem.size > 0) {
                    long rel = byteOffset - f.offset;
                    long idx = rel / f.type.elem.size;
                    long rem = rel % f.type.elem.size;
                    if (idx < 0 || idx >= oa.length) {
                        throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                                "reflect: array-of-struct element index " + idx + " out of range [0, "
                                        + oa.length + ") (D5)");
                    }
                    Object elem = oa[(int) idx];
                    if (!(elem instanceof GoStruct elemStruct)) {
                        throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                                "reflect: array-of-struct element " + idx + " is not a struct (D5)");
                    }
                    return readStructFieldAt(elemStruct, rem, kind);
                }
                if (fv != null && fv.getClass().isArray()) {
                    return readScalarWalk(fv, byteOffset - f.offset, kind);
                }
                // fix-redmaster-r59: a byte offset strictly inside a
                // Long-carried scalar field denotes a sub-word of that
                // field (gc flat memory) — extract the bytes the target
                // kind covers instead of trapping. The ctrlGroup byte
                // read (*(*ctrl)(unsafe.Add(unsafe.Pointer(g), k)))
                // is exactly this shape (byte k of the uint64 ctrl
                // word). Non-Long-carried fields and out-of-word
                // targets keep the loud failure below.
                if (gs.descriptor().field(f.index[0]).inLongs()) {
                    long rel = byteOffset - f.offset;
                    long es = elemSize(kind);
                    if (es > 0 && rel >= 0 && rel + es <= 8) {
                        long bits = gs.getLong(f.index[0]);
                        long mask = es == 8 ? -1L : (1L << (8 * es)) - 1;
                        long extracted = (bits >> (8 * rel)) & mask;
                        return switch (kind) {
                            case INT8 -> Integer.valueOf((int) (byte) extracted);
                            case INT16 -> Integer.valueOf((int) (short) extracted);
                            case INT32, UINT8, UINT16, UINT32 -> Integer.valueOf((int) extracted);
                            case INT64, UINT64 -> Long.valueOf(extracted);
                            case FLOAT32 -> Float.valueOf(Float.intBitsToFloat((int) extracted));
                            case FLOAT64 -> Double.valueOf(Double.longBitsToDouble(extracted));
                            case BOOL -> Boolean.valueOf(extracted != 0);
                            default -> throw new graalgo.values.UnsafeHandles
                                    .UnsafeEscapeException("reflect: byte offset " + byteOffset
                                            + " inside non-aggregate field " + f.name
                                            + " of struct " + info.name + " (D5)");
                        };
                    }
                }
                throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                        "reflect: byte offset " + byteOffset + " inside non-aggregate field "
                                + f.name + " of struct " + info.name + " (D5)");
            }
        }
        throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                "reflect: no field at byte offset " + byteOffset + " of struct " + info.name + " (D5)");
    }

    /**
     * Descends an exact field-boundary match into the container's first
     * element (fix-redmaster-r59 (d)): an ARRAY-of-STRUCT field's element
     * 0, or a STRUCT field's field 0 — but only when that first field's
     * kind is the requested kind (a wrong-kind descent would return a
     * misdecoded value where the caller expects the container). Returns
     * null when the field is not such a container or the descent does
     * not resolve (the caller keeps the field read).
     */
    private static Object descendExactMatch(GoStruct gs, ReflectTypes.FieldInfo f, GoKind kind) {
        GoStruct target = null;
        if (f.type.kind == GoKind.ARRAY && f.type.elem != null
                && f.type.elem.kind == GoKind.STRUCT) {
            Object fv = ReflectValues.structFieldGet(gs, f.index[0], GoKind.ARRAY);
            if (fv instanceof Object[] oa && oa.length > 0 && oa[0] instanceof GoStruct elemStruct) {
                target = elemStruct;
            }
        } else if (f.type.kind == GoKind.STRUCT) {
            Object fv = ReflectValues.structFieldGet(gs, f.index[0], GoKind.STRUCT);
            if (fv instanceof GoStruct nested) {
                target = nested;
            }
        }
        if (target == null || offsetZeroKind(target) != kind) {
            return null;
        }
        return readStructFieldAt(target, 0, kind);
    }

    /** The kind of the field at offset 0 of a struct, or null when unknown. */
    private static GoKind offsetZeroKind(GoStruct gs) {
        ReflectTypes.TypeInfo info = ReflectTypes.infoForName(gs.descriptor().typeName());
        if (info == null) {
            info = ReflectTypes.infoForStructString(gs.descriptor().typeName());
        }
        if (info == null || info.fields == null) {
            return null;
        }
        for (ReflectTypes.FieldInfo f : info.fields) {
            if (f.type != null && f.index.length == 1 && f.offset == 0) {
                return f.type.kind;
            }
        }
        return null;
    }

    private static Object readScalarAt(Object root, long byteOffset, GoKind kind) {
        Object r = root;
        if (r instanceof Cell c) {
            r = c.value;
        } else if (r instanceof graalgo.values.GoCell gc) {
            r = gc.get();
        }
        Object array = r;
        long baseIndex = 0;
        if (r instanceof UnsafeTable.ArrayRef ar) {
            array = ar.array();
            baseIndex = ar.offset();
        } else if (r instanceof GoSlice sl) {
            array = sl.array();
            baseIndex = sl.offset();
        }
        if (array == null || !array.getClass().isArray()) {
            throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                    "reflect: cannot read scalar at byte offset " + byteOffset
                            + " through a non-array root (D5)");
        }
        // fix-redmaster-r51: a bare scalar box is normalized by the
        // table into a single-element carrier array (Integer -> int[1],
        // Long -> long[1], ...), and Value.Interface() on a derived
        // element re-interns the read-out box, so a scalar Value can
        // denote its carrier rather than array storage. Reading the
        // carrier as the target kind's array (arrayGet) casts by kind
        // and dies when the box's carrier differs from the kind's
        // (uint8 element read from an int[1] carrier: [I -> [B). A
        // carrier at offset 0 IS the scalar: unwrap it (deref) instead
        // of walking it as an array. Genuine single-element arrays
        // read identically either way (element 0 is the value), so
        // only the carrier/kind-mismatch case changes behavior.
        // Nonzero offsets keep the walk (a derived byte offset into a
        // carrier is a reinterpretation and still traps below).
        if (baseIndex == 0 && byteOffset == 0) {
            Object unwrapped = deref(array);
            if (unwrapped != array) {
                return unwrapped;
            }
        }
        if (baseIndex != 0) {
            // An ArrayRef/GoSlice root names an element, not byte 0:
            // a further byte offset is gc flat-memory arithmetic on
            // that element, not a fresh element read.
            if (byteOffset == 0) {
                return readScalarAtElement(array, (int) baseIndex, kind);
            }
            Object rawElem = null;
            if (array instanceof Object[] oa) {
                if (baseIndex < 0 || baseIndex >= oa.length) {
                    throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                            "reflect: element index " + baseIndex + " out of range [0, "
                                    + oa.length + ") (D5)");
                }
                rawElem = oa[(int) baseIndex];
            }
            // A further byte offset into a selected element is only
            // meaningful for array elements; re-enter the walk on the
            // element itself when it is an array.
            if (rawElem != null && rawElem.getClass().isArray()) {
                return readScalarWalk(rawElem, byteOffset, kind);
            }
            // An offset into a selected struct element names the
            // field at that offset within the element.
            if (rawElem instanceof GoStruct gsElem) {
                return readStructFieldAt(gsElem, byteOffset, kind);
            }
            // Flat element arithmetic on primitive-carried arrays: an
            // element address plus N bytes names a later element
            // (snapshot 05: long[1] + 8 bytes = long[2]).
            if (!(array instanceof Object[])) {
                long es = elemSize(kind);
                if (es > 0 && byteOffset % es == 0) {
                    return readScalarAtElement(array, (int) (baseIndex + byteOffset / es), kind);
                }
            }
            throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                    "reflect: byte offset " + byteOffset + " into a non-array element (D5)");
        }
        return readScalarWalk(array, byteOffset, kind);
    }

    /** Reads the scalar of {@code kind} at a byte offset into a Java array. */
    private static Object readScalarWalk(Object array, long byteOffset, GoKind kind) {
        Object cur = array;
        long off = byteOffset;
        while (true) {
            if (!cur.getClass().isArray()) {
                throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                        "reflect: scalar walk reached a non-array (D5)");
            }
            int len = java.lang.reflect.Array.getLength(cur);
            if (len == 0) {
                throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                        "reflect: cannot read scalar at offset " + off + " of an empty array (D5)");
            }
            if (cur instanceof Object[] oa) {
                Object first = oa[0];
                if (first != null && first.getClass().isArray()) {
                    long es = flatSize(first);
                    if (es <= 0) {
                        throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                                "reflect: cannot size nested array elements (D5: shape-opaque root)");
                    }
                    long index = off / es;
                    long rem = off % es;
                    if (index < 0 || index >= len) {
                        throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                                "reflect: byte-derived element index " + index + " out of range [0, "
                                        + len + ") (D5: derivation cannot leave its referent)");
                    }
                    cur = oa[(int) index];
                    off = rem;
                    continue;
                }
                // Non-array elements (e.g. []any boxes): only offset 0
                // is meaningful, and the element must carry the kind.
                if (off != 0) {
                    throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                            "reflect: byte offset " + off + " into a non-array element (D5)");
                }
                return boxedScalar(oa[0], kind);
            }
            long es = elemSize(kind);
            if (es <= 0) {
                throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                        "reflect: scalar kind " + kind + " has no element size (D5)");
            }
            if (off % es != 0) {
                throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                        "reflect: byte offset " + off + " is not a multiple of the target "
                                + "element size " + es + " (D5: misaligned derivation traps)");
            }
            long index = off / es;
            if (index < 0 || index >= len) {
                throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                        "reflect: byte-derived element index " + index + " out of range [0, "
                                + len + ") (D5: derivation cannot leave its referent)");
            }
            return ReflectValues.arrayGet(cur, normalizeKind(kind), (int) index);
        }
    }

    /** Reads the scalar of {@code kind} at element {@code index} of an array. */
    private static Object readScalarAtElement(Object array, int index, GoKind kind) {
        if (array == null || !array.getClass().isArray()) {
            throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                    "reflect: cannot read element of a non-array (D5)");
        }
        int len = java.lang.reflect.Array.getLength(array);
        if (index < 0 || index >= len) {
            throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                    "reflect: element index " + index + " out of range [0, " + len + ") (D5)");
        }
        if (array instanceof Object[] oa) {
            Object elem = oa[index];
            if (elem != null && elem.getClass().isArray()) {
                return readScalarWalk(elem, 0, kind);
            }
            return boxedScalar(elem, kind);
        }
        // fix-redmaster-r51: single-element carrier at index 0 (see
        // readScalarAt above) — unwrap instead of casting by kind.
        if (index == 0) {
            Object unwrapped = deref(array);
            if (unwrapped != array) {
                return unwrapped;
            }
        }
        return ReflectValues.arrayGet(array, normalizeKind(kind), index);
    }

    /** A boxed scalar element (e.g. []any) read as the given kind. */
    private static Object boxedScalar(Object elem, GoKind kind) {
        if (elem == null) {
            throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                    "reflect: cannot read scalar from a null element (D5)");
        }
        GoKind k = normalizeKind(kind);
        boolean ok = (elem instanceof Long && (k == GoKind.INT64 || k == GoKind.UINT64))
                || (elem instanceof Integer && (k == GoKind.INT32 || k == GoKind.UINT32))
                || (elem instanceof Short && (k == GoKind.INT16 || k == GoKind.UINT16))
                || (elem instanceof Byte && (k == GoKind.INT8 || k == GoKind.UINT8))
                || (elem instanceof Boolean && k == GoKind.BOOL)
                || (elem instanceof Float && k == GoKind.FLOAT32)
                || (elem instanceof Double && k == GoKind.FLOAT64);
        if (!ok) {
            throw new graalgo.values.UnsafeHandles.UnsafeEscapeException(
                    "reflect: element " + elem.getClass().getSimpleName()
                            + " does not carry scalar kind " + kind + " (D5)");
        }
        return elem;
    }

    /**
     * Flat Go byte size of a Java array value, or -1 when the shape is
     * opaque (struct elements, empty arrays, null first element).
     * Mirrors UnsafeToPtrNode's element-size logic for the array shapes
     * a scalar walk can descend (rectangularity holds for every Go
     * array value).
     */
    private static long flatSize(Object array) {
        if (array == null || !array.getClass().isArray()) {
            return -1;
        }
        if (array instanceof long[] la) {
            return (long) la.length * 8;
        }
        if (array instanceof int[] ia) {
            return (long) ia.length * 4;
        }
        if (array instanceof short[] sa) {
            return (long) sa.length * 2;
        }
        if (array instanceof byte[] ba) {
            return ba.length;
        }
        if (array instanceof boolean[] ba) {
            return ba.length;
        }
        if (array instanceof float[] fa) {
            return (long) fa.length * 4;
        }
        if (array instanceof double[] da) {
            return (long) da.length * 8;
        }
        if (array instanceof Object[] oa) {
            if (oa.length == 0 || oa[0] == null) {
                return -1;
            }
            long inner = flatSize(oa[0]);
            return inner <= 0 ? -1 : (long) oa.length * inner;
        }
        return -1;
    }

    /** Scalar kinds whose value at an address is a single element read. */
    private static boolean isScalarKind(GoKind kind) {
        return switch (kind) {
            case INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64,
                    FLOAT32, FLOAT64, BOOL, COMPLEX64, COMPLEX128,
                    UNTYPED_INT, UNTYPED_FLOAT, UNTYPED_COMPLEX, UNTYPED_BOOL -> true;
            default -> false;
        };
    }

    /** Untyped kinds read with their default concrete carrier. */
    private static GoKind normalizeKind(GoKind kind) {
        return switch (kind) {
            case UNTYPED_INT -> GoKind.INT64;
            case UNTYPED_FLOAT -> GoKind.FLOAT64;
            case UNTYPED_COMPLEX -> GoKind.COMPLEX128;
            case UNTYPED_BOOL -> GoKind.BOOL;
            default -> kind;
        };
    }

    /** Byte size of one scalar of the given kind, or -1 when not scalar-sized. */
    private static long elemSize(GoKind kind) {
        return switch (normalizeKind(kind)) {
            case INT8, UINT8, BOOL -> 1;
            case INT16, UINT16 -> 2;
            case INT32, UINT32, FLOAT32 -> 4;
            case INT64, UINT64, FLOAT64, COMPLEX64 -> 8;
            case COMPLEX128 -> 16;
            default -> -1;
        };
    }

    /**
     * True when the referent is a mutable cell (a {@link Cell} or a
     * mutable aggregate): stores through this handle are meaningful.
     */
    public static boolean isCellHandle(long handle) {
        return resolve(handle) instanceof Cell;
    }
}
