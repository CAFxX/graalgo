package graalgo.p3reflect;

import graalgo.frontend.Boxing;
import graalgo.frontend.FrontendException;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * Allocator intrinsics for the bodyless runtime linknames
 * {@code internal/reflectlite.unsafe_New}, {@code reflect.unsafe_New} and
 * {@code reflect.unsafe_NewArray} (RV-P1-02, F4).
 *
 * <p>gc: {@code unsafe_New(t)} is {@code newobject(t)} — it allocates a
 * zeroed object of the type described by {@code t} and returns a pointer
 * to it. {@code unsafe_NewArray(t, n)} allocates a zeroed {@code [n]t}
 * and returns a pointer to the first element.
 *
 * <p>D5 mapping:
 * <ul>
 *   <li>The allocated object is the token's zero D5 value, reusing
 *       {@link ReflectOpNode#zeroFor}'s kind dispatch (the same zeroing
 *       the {@code __reflect_new} intrinsic uses).</li>
 *   <li>The storage is a fresh {@link GoBox} cell per allocation — the
 *       same storage model as {@code new(T)} scalars (NewNode, P3-M9 /
 *       P3-M10): the cell is the allocation's storage identity, so the
 *       {@code unsafe.Pointer} handle stays stable across {@code *p = v}
 *       writes, and distinct allocations mint distinct handles
 *       (UnsafeTable interns by identity).</li>
 *   <li>The intrinsic returns the {@link UnsafeTable} handle (D5:
 *       {@code unsafe.Pointer} is a Long), so the result flows through
 *       the existing {@code unsafetooptr}/{@code unsafefromptr} machinery
 *       exactly like any other {@code unsafe.Pointer}.</li>
 *   <li>{@code unsafe_NewArray} returns a handle to an
 *       {@link UnsafeTable.ArrayRef} at element 0 (the D5 element-pointer
 *       model, as for {@code &arr[i]}), so {@code (*T)(p)} addresses the
 *       first element.</li>
 * </ul>
 *
 * <p>All allocations are ordinary Java objects: zeroed by construction,
 * visible to the GC, with fresh identity per call. Nothing is faked.
 */
public final class AllocIntrinsics {
    private AllocIntrinsics() {}

    /**
     * D5 carrier contract for {@code *abi.Type} argument values. The
     * argument arrives as a {@link GoPointer} whose referent implements
     * this interface; the token is the canonical reflect type token
     * (the same namespace {@link ReflectTypes} uses).
     *
     * <p>The carrier itself is produced by the eface-view mechanism
     * (P3-M10): {@code internal/abi.TypeOf} synthesizes the
     * {@code *abi.Type} view over an interface value's dynamic type.
     * Until that mechanism is present, no {@code *abi.Type} value can
     * reach the backend (the eface reinterpretation has no D5 support
     * without it), so a non-carrier argument fails loud here — never a
     * silent wrong allocation.
     */
    public interface AbiTypeCarrier {
        /** The canonical reflect type token. */
        long tok();
    }

    /**
     * {@code unsafe_New(t *abi.Type) unsafe.Pointer}: allocates a zeroed
     * object of the type described by {@code t}; returns the
     * {@code unsafe.Pointer} handle (Long).
     */
    public static Object unsafeNew(Object typeArg) {
        return unsafeNewForToken(tokOf(typeArg));
    }

    /**
     * {@code unsafe_NewArray(t *abi.Type, n int) unsafe.Pointer}:
     * allocates a zeroed {@code [n]t}; returns the handle addressing
     * element 0 (Long).
     */
    public static Object unsafeNewArray(Object typeArg, Object nArg) {
        int n = ((Number) nArg).intValue();
        return unsafeNewArrayForToken(tokOf(typeArg), n);
    }

    /**
     * Genuine allocation core, directly unit-testable: token → zeroed,
     * GC-managed object → stable {@code unsafe.Pointer} handle.
     *
     * @return the UnsafeTable handle (D5 {@code unsafe.Pointer} = Long)
     */
    public static long unsafeNewForToken(long tok) {
        // Validate the token first: unknown tokens fail loud, never
        // allocate a mistyped object.
        ReflectTypes.table().info(tok);
        Object zero = ReflectOpNode.zeroFor(tok);
        // Fresh cell per allocation (NewNode P3-M9/P3-M10): the cell is
        // the storage identity. zeroFor may return JVM-cached boxes for
        // scalars (Long.valueOf(0L)); the GoBox itself is always fresh,
        // so distinct allocations never share a handle.
        GoBox cell = new GoBox(zero);
        return UnsafeTable.current().intern(cell);
    }

    /**
     * Array allocation core: zeroed {@code [n]elem}; returns the handle
     * addressing element 0.
     *
     * @return the UnsafeTable handle (D5 {@code unsafe.Pointer} = Long)
     */
    public static long unsafeNewArrayForToken(long tok, int n) {
        if (n < 0) {
            throw new FrontendException("unsafe_NewArray: negative length " + n);
        }
        ReflectTypes.TypeInfo elem = ReflectTypes.table().info(tok);
        Object arr = zeroedArray(elem, n);
        return UnsafeTable.current().internEqual(new UnsafeTable.ArrayRef(arr, 0));
    }

    /** Zeroed Java array for n elements of the token's kind. */
    private static Object zeroedArray(ReflectTypes.TypeInfo elem, int n) {
        GoKind kind = elem.kind;
        if (kind == GoKind.STRUCT) {
            // Struct elements: fresh recursive zero per element (zeroFor
            // handles nested structs/arrays; Boxing.newArray's legacy
            // 2-arg form would leave struct slots null).
            Object[] arr = new Object[n];
            for (int i = 0; i < n; i++) {
                arr[i] = ReflectOpNode.zeroFor(elem.tok);
            }
            return arr;
        }
        return Boxing.newArray(kind, null, n);
    }

    /** Extracts the reflect type token from the {@code *abi.Type} argument. */
    private static long tokOf(Object typeArg) {
        Object ref = typeArg instanceof GoPointer gp ? gp.referent() : typeArg;
        if (ref instanceof AbiTypeCarrier c) {
            return c.tok();
        }
        throw new FrontendException(
                "unsafe_New: *abi.Type argument does not carry a type token "
                        + "(got " + (ref == null ? "null" : ref.getClass().getSimpleName()) + "); "
                        + "the eface-view mechanism (P3-M10) that synthesizes *abi.Type "
                        + "values is required");
    }
}
