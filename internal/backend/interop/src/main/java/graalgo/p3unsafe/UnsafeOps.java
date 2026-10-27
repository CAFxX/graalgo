package graalgo.p3unsafe;

import com.oracle.truffle.api.nodes.Node;
import graalgo.frontend.GoFieldCell;
import graalgo.runtime.Intrinsics;
import graalgo.runtime.Panic;
import graalgo.values.GoCell;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.UnsafeHandles;

/**
 * Referent-shape dispatch for the {@code unsafe} runtime intrinsics
 * (P3-M8). <b>Frozen contract</b> with {@link UnsafeTable}: the shapes
 * below are the complete enumeration of referents the handle model can
 * represent (R-US-1). Anything else fails fast and loud — never silently.
 *
 * <p>Accepted shapes per intrinsic:
 * <ul>
 *   <li>{@link #slice}: {@code null} (nil pointer: len 0 → nil slice),
 *       a whole Java typed array, an {@link UnsafeTable.ArrayRef}
 *       ({@code SliceData} view), a {@code GoSlice} (sub-slice view), or
 *       a bare scalar box (a cell pointer's content: {@code new(T)} /
 *       {@code &x} scalars — normalized to its stable carrier, P3-M10).
 *       The array's component type must be {@code T}'s carrier — the
 *       handle model cannot reinterpret memory, so a mismatch fails fast
 *       instead of aliasing the wrong type.</li>
 *   <li>{@link #stringOf}: {@code null} (len 0 → ""), a {@code GoString},
 *       a {@code byte[]} / {@code int[]} (the {@code []byte} carrier), an
 *       {@link UnsafeTable.ArrayRef}, or a {@code GoSlice}. Always copies
 *       (DIV-US-1: {@code GoString} is immutable; post-call writes through
 *       the original pointer are not visible in the string, unlike gc).</li>
 *   <li>{@link #sliceData}: a {@code GoSlice} (or {@code null} → handle 0).
 *       Interns an {@link UnsafeTable.ArrayRef} key, so
 *       {@code SliceData(s) == SliceData(s)} while
 *       {@code SliceData(s) != SliceData(s[1:])} (gc parity).</li>
 *   <li>{@link #stringData}: a {@code GoString}; empty → handle 0
 *       (gc: {@code StringData("") == nil}).</li>
 * </ul>
 *
 * <p>Failure taxonomy: bounds/nil violations that gc also rejects panic
 * with gc-exact {@code "runtime error: ..."} messages (fully differential);
 * shapes the handle model cannot represent panic with an
 * {@code "unsafe.<op>: ..."} message (intentional, documented divergence —
 * gc would silently reinterpret); D5 traps ({@link #addTrap},
 * {@link #fromUintTrap}) throw {@link UnsafeHandles.UnsafeEscapeException}.
 * The {@code unsafe.Pointer → uintptr} direction does <em>not</em> trap:
 * it yields the handle as an integer identity token (PKG-007; see
 * {@link graalgo.values.GoConvert#unsafePointerToUintptr}) — the token is
 * opaque and comparable but never materializes back into a handle, so the
 * traps above keep holding.
 */
public final class UnsafeOps {
    private UnsafeOps() {}

    /** Array length via reflection (any component type). */
    static int arrayLength(Object array) {
        return java.lang.reflect.Array.getLength(array);
    }

    /**
     * The Java array class carrying {@code T}'s values — mirrors
     * {@code Intrinsics.newTypedArray} exactly (twin switch; the two must
     * stay in lockstep: a mismatch makes {@code unsafe.Slice} hand
     * downstream indexing an array of the wrong component type).
     */
    static Class<?> expectedArrayClass(GoKind elemKind) {
        return switch (elemKind) {
            case INT8, UINT8 -> byte[].class;
            case INT16, UINT16 -> short[].class;
            case INT32, UINT32 -> int[].class;
            case INT64, UINT64, UNTYPED_INT -> long[].class;
            case FLOAT32 -> float[].class;
            case FLOAT64, UNTYPED_FLOAT -> double[].class;
            case BOOL, UNTYPED_BOOL -> boolean[].class;
            case COMPLEX64 -> GoComplex32[].class;
            case COMPLEX128, UNTYPED_COMPLEX -> GoComplex64[].class;
            default -> Object[].class;
        };
    }

    private static void checkCarrier(Object array, GoKind elemKind, String op, Node site) {
        if (array.getClass() != expectedArrayClass(elemKind)) {
            Panic.throwPanic("unsafe." + op + ": cannot view "
                    + array.getClass().getSimpleName() + " as []" + elemKind
                    + " (D5: the handle model cannot reinterpret memory)",
                    null, null, site);
        }
    }

    /**
     * Opens a raw pointer referent for the view intrinsics ({@link #slice},
     * {@link #stringOf}): a bare scalar box — what a cell pointer's
     * {@code referent()} yields for {@code new(T)}/{@code &x} scalars —
     * normalizes to its stable single-element carrier via the table's
     * {@code normalizeScalar} cache (the same carrier object across calls,
     * so repeated views alias, corpus 05). P3-M10: this normalization used
     * to happen at {@code UnsafeTable.intern} time; interning now keys on
     * the cell itself, so views normalize here instead. Everything else
     * passes through unchanged.
     */
    private static Object openScalar(Object referent) {
        return UnsafeTable.current().normalizeScalar(referent);
    }

    /**
     * {@code unsafe.Slice(ptr, len)} given the pointer's referent (null for
     * a nil pointer). Returns the new {@code GoSlice}, or {@code null} for
     * the nil slice.
     */
    public static GoSlice slice(Object referent, long len, GoKind elemKind, Node site) {
        if (len < 0) {
            Panic.throwPanic("runtime error: unsafe.Slice: len out of range", null, null, site);
        }
        // P3-M10: cell-pointer scalars arrive as bare boxes; open them to
        // their stable carrier before the shape dispatch.
        referent = openScalar(referent);
        if (referent == null) {
            if (len != 0) {
                Panic.throwPanic("runtime error: unsafe.Slice: ptr is nil and len is not zero",
                        null, null, site);
            }
            return null;
        }
        Object array;
        int baseOffset;
        int cap;
        if (referent instanceof UnsafeTable.ArrayRef ref) {
            array = ref.array();
            baseOffset = ref.offset();
            cap = arrayLength(array) - baseOffset;
        } else if (referent instanceof GoSlice gs) {
            array = gs.array();
            baseOffset = gs.offset();
            cap = gs.cap();
        } else if (referent.getClass().isArray()) {
            array = referent;
            baseOffset = 0;
            cap = arrayLength(array);
        } else {
            Panic.throwPanic("unsafe.Slice: unsupported referent shape "
                    + referent.getClass().getSimpleName(), null, null, site);
            throw new AssertionError("unreachable");
        }
        checkCarrier(array, elemKind, "Slice", site);
        if (len > cap) {
            Panic.throwPanic("unsafe.Slice: len out of range", null, null, site);
        }
        return new GoSlice(array, baseOffset, (int) len, cap);
    }

    /**
     * {@code unsafe.SliceData(s)}: interns a view key for the slice's
     * backing array at its offset. {@code null} (nil slice) → handle 0.
     */
    public static long sliceData(GoSlice s) {
        if (s == null) {
            return 0;
        }
        return UnsafeTable.current()
                .internEqual(new UnsafeTable.ArrayRef(s.array(), s.offset()));
    }

    /**
     * {@code unsafe.String(ptr, len)} given the pointer's referent (null
     * for a nil pointer). Always copies (DIV-US-1).
     */
    public static GoString stringOf(Object referent, long len, Node site) {
        if (len < 0) {
            Panic.throwPanic("runtime error: unsafe.String: len out of range", null, null, site);
        }
        // P3-M10: see slice() — cell-pointer scalars arrive as bare boxes.
        referent = openScalar(referent);
        if (referent == null) {
            if (len != 0) {
                Panic.throwPanic("runtime error: unsafe.String: ptr is nil and len is not zero",
                        null, null, site);
            }
            return GoString.EMPTY;
        }
        int n = (int) len;
        // A GoString referent: reachable via a StringData -> String round-trip
        // (StringData interns the GoString; String reads it back without an
        // intervening unsafe.Pointer). Copies per DIV-US-1.
        if (referent instanceof GoString gs) {
            byte[] bytes = gs.bytes();
            if (n > bytes.length) {
                Panic.throwPanic("unsafe.String: len out of range", null, null, site);
            }
            byte[] out = new byte[n];
            System.arraycopy(bytes, 0, out, 0, n);
            return new GoString(out);
        }
        Object array;
        int baseOffset;
        if (referent instanceof UnsafeTable.ArrayRef ref) {
            array = ref.array();
            baseOffset = ref.offset();
        } else if (referent instanceof GoSlice gs) {
            array = gs.array();
            baseOffset = gs.offset();
        } else if (referent.getClass().isArray()) {
            array = referent;
            baseOffset = 0;
        } else {
            Panic.throwPanic("unsafe.String: unsupported referent shape "
                    + referent.getClass().getSimpleName(), null, null, site);
            throw new AssertionError("unreachable");
        }
        if (array instanceof byte[] bytes) {
            if (n > bytes.length - baseOffset) {
                Panic.throwPanic("unsafe.String: len out of range", null, null, site);
            }
            byte[] out = new byte[n];
            System.arraycopy(bytes, baseOffset, out, 0, n);
            return new GoString(out);
        }
        if (array instanceof int[] ints) {
            if (n > ints.length - baseOffset) {
                Panic.throwPanic("unsafe.String: len out of range", null, null, site);
            }
            byte[] out = new byte[n];
            for (int i = 0; i < n; i++) {
                out[i] = (byte) ints[baseOffset + i];
            }
            return new GoString(out);
        }
        Panic.throwPanic("unsafe.String: cannot view "
                + array.getClass().getSimpleName()
                + " as bytes (D5: the handle model cannot reinterpret memory)",
                null, null, site);
        throw new AssertionError("unreachable");
    }

    /**
     * {@code unsafe.StringData(s)}: interns the string's {@code GoString}.
     * Empty → handle 0 (gc: {@code StringData("") == nil}).
     */
    public static long stringData(GoString s) {
        if (s.length() == 0) {
            return 0;
        }
        return UnsafeTable.current().intern(s);
    }

    /**
     * {@code unsafe.Add} always traps (D5/GG-0015): handles admit no byte
     * arithmetic. Re-test trigger: a byte-addressed value model (no plan).
     */
    public static void addTrap() {
        throw new UnsafeHandles.UnsafeEscapeException(
                "unsafe.Add: pointer arithmetic on unsafe.Pointer traps "
                        + "(D5: handles admit no byte arithmetic; "
                        + "re-test: byte-addressed value model)");
    }

    /**
     * {@code uintptr → unsafe.Pointer} always traps (D5): a handle can
     * never be materialized from an integer.
     */
    public static void fromUintTrap() {
        throw new UnsafeHandles.UnsafeEscapeException(
                "unsafe: uintptr -> unsafe.Pointer conversion traps "
                        + "(D5: a handle can never be materialized from an integer; "
                        + "re-test: byte-addressed value model)");
    }

    /**
     * Element-indexed pointer arithmetic for the {@code "unsafeptradd"} wire
     * node (fix-redmaster-abifuncpc): {@code unsafe.Pointer(uintptr(p) +
     * k*unsafe.Sizeof(T))}.
     *
     * <p>The base {@code ptrHandle} must resolve to an {@link
     * UnsafeTable.ArrayRef} — the storage-identity interning in {@link
     * UnsafeFromPtrNode} produces these for {@code &arr[i]} element pointers
     * and {@code SliceData} views. The result is a new {@code ArrayRef}
     * {@code k} elements forward, interned by value equality (so the same
     * (array, index) always yields the same handle, matching gc pointer
     * identity).
     *
     * <p>This is not D5 byte arithmetic: the base is a proven-valid handle
     * (the frontend matched {@code uintptr(p)} with {@code p:
     * unsafe.Pointer}, not an arbitrary integer) and {@code k} is a
     * proven-whole element count (the frontend matched the
     * {@code k*unsafe.Sizeof(T)} factor). A non-ArrayRef base or an
     * out-of-range index traps — the D5 discipline is unchanged.
     */
    public static long ptrAdd(long ptrHandle, long index) {
        Object referent = UnsafeTable.current().resolve(ptrHandle);
        if (!(referent instanceof UnsafeTable.ArrayRef ar)) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptradd: base is not an array element reference "
                            + "(D5: no arithmetic on opaque handles)");
        }
        long newOffset = (long) ar.offset() + index;
        if (newOffset < 0 || newOffset > Integer.MAX_VALUE) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptradd: element index out of range: " + newOffset);
        }
        // Bounds-check against the backing array length. The ArrayRef offset
        // is an element index; the array is a Java array.
        int len = java.lang.reflect.Array.getLength(ar.array());
        if (newOffset >= len) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptradd: element index " + newOffset
                            + " out of bounds (len " + len + ")");
        }
        return UnsafeTable.current().internEqual(
                new UnsafeTable.ArrayRef(ar.array(), (int) newOffset));
    }

    /**
     * Byte-offset pointer arithmetic for the {@code "unsafeptraddbytes"}
     * wire node (fix-redmaster-r27): {@code unsafe.Pointer(uintptr(p) + x)}
     * for a general byte offset {@code x}.
     *
     * <p>The base {@code ptrHandle} must be a proven-valid handle (the
     * frontend matched {@code uintptr(p)} with {@code p: unsafe.Pointer},
     * not an arbitrary integer), so the D5 discipline is unchanged for
     * genuinely integer-derived pointers (those still trap via
     * {@code "unsafefromuint"}). The byte offset is resolved against the
     * base referent's layout: a struct field (recursing into nested
     * struct/array/slice fields for interior offsets), an array/slice
     * element (with alignment and bounds checks), or a cell's contents.
     * Nil bases, scalar bases, misaligned or out-of-range offsets, and
     * one-past-end struct offsets trap — fail closed, never silently
     * aliasing the wrong storage.
     *
     * <p>Array element sizes come from the carrier, not from a name or a
     * type-table lookup the handle model does not carry: primitive Java
     * arrays have Java size == Go size for every graalgo primitive
     * carrier, and Object arrays are homogeneous, so one representative
     * element determines the element size (recursing into nested arrays).
     * Empty or all-nil Object arrays trap: their element type is
     * unknowable from contents.
     */
    public static long ptrAddBytes(long ptrHandle, long byteOffset) {
        Object referent = UnsafeTable.current().resolve(ptrHandle);
        if (referent == null) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptraddbytes: nil base pointer "
                            + "(D5: no arithmetic on opaque handles)");
        }
        if (byteOffset == 0) {
            // Offset 0 is the identity — EXCEPT for struct bases, where
            // the struct pointer itself is not dereferenceable as a field
            // type (reflect's Field(0) → add(ptr, 0) must yield a field-0
            // cell, or the field read below fails unboxing the struct).
            // A cell holding a struct is the same case (the cell's
            // contents, not the cell, is the struct). This also covers
            // zero-sized element types, whose offset is always 0.
            if (referent instanceof GoStruct gs) {
                return UnsafeTable.current().intern(structAtByteOffset(gs, 0));
            }
            if (referent instanceof GoCell cell && cell.get() instanceof GoStruct gs) {
                return UnsafeTable.current().intern(structAtByteOffset(gs, 0));
            }
            return ptrHandle;
        }
        Object target = resolveByteTarget(referent, byteOffset);
        // ArrayRefs intern by value equality (same (array, index) -> same
        // handle, matching gc pointer identity); cells intern by identity.
        if (target instanceof UnsafeTable.ArrayRef ar) {
            return UnsafeTable.current().internEqual(ar);
        }
        return UnsafeTable.current().intern(target);
    }

    /** Resolves a byte offset against one referent shape (fail-closed). */
    private static Object resolveByteTarget(Object referent, long byteOffset) {
        if (referent instanceof UnsafeTable.ArrayRef ar) {
            return arrayAtByteOffset(ar.array(), ar.offset(), byteOffset);
        }
        if (referent instanceof GoCell cell) {
            Object value = cell.get();
            if (value instanceof GoStruct gs) {
                return structAtByteOffset(gs, byteOffset);
            }
            if (value instanceof GoSlice gs) {
                return arrayAtByteOffset(gs.array(), gs.offset(), byteOffset);
            }
            if (value != null && value.getClass().isArray()) {
                return arrayAtByteOffset(value, 0, byteOffset);
            }
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptraddbytes: cell contents are not byte-addressable: "
                            + (value == null ? "null" : value.getClass().getSimpleName()));
        }
        if (referent instanceof GoStruct gs) {
            return structAtByteOffset(gs, byteOffset);
        }
        if (referent instanceof GoSlice gs) {
            return arrayAtByteOffset(gs.array(), gs.offset(), byteOffset);
        }
        if (referent.getClass().isArray()) {
            return arrayAtByteOffset(referent, 0, byteOffset);
        }
        throw new UnsafeHandles.UnsafeEscapeException(
                "unsafeptraddbytes: base is not a byte-addressable referent: "
                        + referent.getClass().getSimpleName());
    }

    /**
     * Element-indexed view of a byte offset into a Java array: the Go
     * element size comes from the array's carrier (see {@link
     * #ptrAddBytes}).
     */
    private static UnsafeTable.ArrayRef arrayAtByteOffset(Object array, int baseOffset, long byteOffset) {
        long elemSize = goElemSize(array);
        if (elemSize <= 0) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptraddbytes: cannot determine element size for "
                            + array.getClass().getSimpleName());
        }
        if (byteOffset % elemSize != 0) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptraddbytes: misaligned byte offset " + byteOffset
                            + " for element size " + elemSize);
        }
        long newOffset = (long) baseOffset + byteOffset / elemSize;
        int len = java.lang.reflect.Array.getLength(array);
        if (newOffset < 0 || newOffset >= len) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptraddbytes: element index " + newOffset
                            + " out of bounds (len " + len + ")");
        }
        return new UnsafeTable.ArrayRef(array, (int) newOffset);
    }

    /** Go element size in bytes for a Java array's elements (0 = unknown). */
    private static long goElemSize(Object array) {
        Class<?> comp = array.getClass().getComponentType();
        if (comp == null) {
            return 0;
        }
        if (comp.isPrimitive()) {
            // Every graalgo primitive carrier has Java size == Go size.
            return switch (comp.getSimpleName()) {
                case "byte", "boolean" -> 1;
                case "char", "short" -> 2;
                case "int", "float" -> 4;
                case "long", "double" -> 8;
                default -> 0;
            };
        }
        if (comp != Object.class) {
            // Not a graalgo carrier shape; fail closed.
            return 0;
        }
        // Object[]: homogeneous Go elements — inspect a representative.
        int len = java.lang.reflect.Array.getLength(array);
        for (int i = 0; i < len; i++) {
            Object elem = java.lang.reflect.Array.get(array, i);
            if (elem != null) {
                return goValueSize(elem);
            }
        }
        return 0; // empty or all-nil: element type unknowable from contents
    }

    /** Go ABI size in bytes of one stored value (0 = unknown). */
    private static long goValueSize(Object value) {
        if (value instanceof GoStruct gs) {
            return Intrinsics.structSizeBytes(gs.descriptor());
        }
        if (value instanceof GoString) {
            return 16;
        }
        if (value instanceof GoSlice) {
            return 24;
        }
        if (value instanceof GoInterface) {
            return 16;
        }
        if (value instanceof GoComplex32) {
            return 8;
        }
        if (value instanceof GoComplex64) {
            return 16;
        }
        if (value.getClass().isArray()) {
            // Nested Go array: length * element size (recursive).
            long elemSize = goElemSize(value);
            return elemSize <= 0 ? 0
                    : (long) java.lang.reflect.Array.getLength(value) * elemSize;
        }
        // Pointers, maps, funcs, chans, and other scalar-carrying objects.
        return 8;
    }

    /**
     * Field view of a byte offset into a GoStruct: walks the ABI field
     * layout (declaration order, natural alignment — mirroring {@link
     * Intrinsics#structSizeBytes}) and returns a {@link GoFieldCell} for
     * the field at the offset, or an {@link UnsafeTable.ArrayRef} for an
     * interior offset into a nested array/slice field, recursing into
     * nested struct fields.
     */
    private static Object structAtByteOffset(GoStruct structVal, long byteOffset) {
        GoStructDescriptor desc = structVal.descriptor();
        long offset = 0;
        int declIdx = 0;
        for (GoStructDescriptor.Field f : desc.fields()) {
            long[] sa = fieldSizeAndAlign(f);
            long fieldSize = sa[0];
            long align = sa[1];
            long fieldStart = (offset + align - 1) / align * align;
            if (byteOffset >= fieldStart && byteOffset < fieldStart + fieldSize) {
                if (byteOffset == fieldStart) {
                    return new GoFieldCell(structVal, declIdx, f.kind());
                }
                return structFieldInterior(structVal, f, declIdx, byteOffset - fieldStart);
            }
            offset = fieldStart + fieldSize;
            declIdx++;
        }
        throw new UnsafeHandles.UnsafeEscapeException(
                "unsafeptraddbytes: byte offset " + byteOffset + " outside struct");
    }

    /** Interior offset into a composite field (nested struct/array/slice). */
    private static Object structFieldInterior(GoStruct structVal, GoStructDescriptor.Field f,
            int declIdx, long inner) {
        if (f.inLongs()) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafeptraddbytes: interior offset into a scalar field");
        }
        Object value = structVal.getObject(declIdx);
        if (value instanceof GoStruct nested) {
            return structAtByteOffset(nested, inner);
        }
        if (value instanceof GoSlice gs) {
            return arrayAtByteOffset(gs.array(), gs.offset(), inner);
        }
        if (value != null && value.getClass().isArray()) {
            return arrayAtByteOffset(value, 0, inner);
        }
        throw new UnsafeHandles.UnsafeEscapeException(
                "unsafeptraddbytes: interior offset into a non-composite field: "
                        + (value == null ? "null" : value.getClass().getSimpleName()));
    }

    /**
     * Returns {@code {size, alignment}} for one struct field, mirroring
     * the runtime {@code Intrinsics} layout rules (whose
     * {@code structSizeAndAlign} is private).
     */
    private static long[] fieldSizeAndAlign(GoStructDescriptor.Field f) {
        long basic = Intrinsics.elemSizeBytes(f.kind());
        if (basic != 0) {
            return new long[]{basic, Math.min(basic, 8)};
        }
        if (f.kind() == GoKind.STRUCT) {
            GoStructDescriptor nested = f.structDesc();
            if (nested == null) {
                throw new UnsafeHandles.UnsafeEscapeException(
                        "unsafeptraddbytes: STRUCT field without nested descriptor");
            }
            return structSizeAndAlign(nested);
        }
        if (f.kind() == GoKind.ARRAY) {
            long[] esa = arrayElemSizeAndAlign(f);
            if (f.arrayLen() < 0) {
                throw new UnsafeHandles.UnsafeEscapeException(
                        "unsafeptraddbytes: ARRAY field without array length");
            }
            return new long[]{(long) f.arrayLen() * esa[0], esa[1]};
        }
        throw new UnsafeHandles.UnsafeEscapeException(
                "unsafeptraddbytes: unsupported field kind " + f.kind());
    }

    /** Returns {@code {size, alignment}} for a struct descriptor. */
    private static long[] structSizeAndAlign(GoStructDescriptor desc) {
        long offset = 0;
        long maxAlign = 1;
        for (GoStructDescriptor.Field f : desc.fields()) {
            long[] sa = fieldSizeAndAlign(f);
            long align = sa[1];
            maxAlign = Math.max(maxAlign, align);
            offset = (offset + align - 1) / align * align;
            offset += sa[0];
        }
        return new long[]{(offset + maxAlign - 1) / maxAlign * maxAlign, maxAlign};
    }

    /** Returns {@code {size, alignment}} for an ARRAY-kind field's element. */
    private static long[] arrayElemSizeAndAlign(GoStructDescriptor.Field f) {
        GoKind ek = f.arrayElemKind();
        long basic = ek == null ? 0 : Intrinsics.elemSizeBytes(ek);
        if (basic != 0) {
            return new long[]{basic, Math.min(basic, 8)};
        }
        if (ek == GoKind.STRUCT && f.arrayElemDesc() != null) {
            return structSizeAndAlign(f.arrayElemDesc());
        }
        throw new UnsafeHandles.UnsafeEscapeException(
                "unsafeptraddbytes: unsupported array element kind " + ek);
    }
}
