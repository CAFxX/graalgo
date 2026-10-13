package graalgo.p3unsafe;

import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Panic;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
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
 * {@link #fromUintTrap}) throw {@link UnsafeHandles.UnsafeEscapeException},
 * like the existing {@code unsafe.Pointer → uintptr} trap.
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
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> int[].class;
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
}
