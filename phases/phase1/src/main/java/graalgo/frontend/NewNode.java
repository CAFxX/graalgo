package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStructDescriptor;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code new} builtin (WS G; verifies GG-0020).
 *
 * <p>{@code new(T)} allocates the zero value of {@code T} and returns a
 * pointer to it. Explicit {@code &x} / {@code *p} remain WS D scope errors;
 * this node is only built for the {@code new} builtin call.
 */
public final class NewNode extends GoExprNode {
    private final GoKind elemKind;
    private final GoStructDescriptor structDesc; // non-null iff elemKind == STRUCT
    private final int[] arrayLens; // lengths per nesting level, outermost first; null iff elemKind != ARRAY
    private final GoKind arrayUltimateElemKind; // non-null iff elemKind == ARRAY
    /** Descriptor for the ultimate struct element (new([N]S)); null unless the ultimate element is a STRUCT. */
    private final GoStructDescriptor ultimateStructDesc;
    /**
     * Recursive zero template for STRUCT/ARRAY element kinds (built by the
     * factory via NodeFactory.zeroTemplateFor); null for scalar kinds.
     * P3-M9: GoStruct.zero (the descriptor-only path) leaves nested struct
     * fields null — a new(T) referent must be the fully recursive zero
     * value, deep-copied per allocation (Boxing.freshZeroCopy).
     */
    private final Object zeroTemplate;

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc) {
        this(elemKind, structDesc, null, null, null, null);
    }

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc, int[] arrayLens, GoKind arrayUltimateElemKind) {
        this(elemKind, structDesc, arrayLens, arrayUltimateElemKind, null, null);
    }

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc,
            int[] arrayLens, GoKind arrayUltimateElemKind, GoStructDescriptor ultimateStructDesc) {
        this(elemKind, structDesc, arrayLens, arrayUltimateElemKind, ultimateStructDesc, null);
    }

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc,
            int[] arrayLens, GoKind arrayUltimateElemKind, GoStructDescriptor ultimateStructDesc,
            Object zeroTemplate) {
        super(GoKind.POINTER);
        this.elemKind = elemKind;
        this.structDesc = structDesc;
        this.arrayLens = arrayLens;
        this.arrayUltimateElemKind = arrayUltimateElemKind;
        this.ultimateStructDesc = ultimateStructDesc;
        this.zeroTemplate = zeroTemplate;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (zeroTemplate != null) {
            // STRUCT/ARRAY referent: fresh deep copy of the recursive zero
            // template — never the shallow GoStruct.zero (null nested fields).
            return new GoPointer(Boxing.freshZeroCopy(zeroTemplate));
        }
        // P3-M8b: each new() must allocate a FRESH referent object. Boxing.zero
        // returns cached shared values (e.g. Long.valueOf(0L)) — if two new(int)
        // calls share the same referent, unsafe.Pointer interning (identity-based)
        // would give them the same handle, breaking p1 != q1 for distinct allocations.
        Object referent = freshZero(elemKind, structDesc, arrayLens, arrayUltimateElemKind, ultimateStructDesc);
        // P3-M9 (ecdsa root fix): a scalar new(T) allocates a fresh GoBox
        // cell (the same storage model as &x), not a single-element carrier
        // array. The carrier convention was half-adopted: DerefWriteNode
        // *replaced* the carrier with the scalar box on the first *p = v,
        // so a *p read before any write (e.g. cryptobyte's
        // `if !*b.inContinuation` on a fresh new(bool)) saw the raw [Z and
        // died in DerefNode.executeBoolean. A cell reads and writes
        // uniformly from birth.
        // P3-M10: the fresh cell is the allocation's storage identity —
        // UnsafeFromPtrNode interns the cell (not the cell's content, which
        // is Boxing.zero's JVM-shared cached box), so distinct new(T)
        // calls mint distinct unsafe.Pointer handles and a pointer keeps
        // its handle across *p = v writes.
        if (referent instanceof graalgo.values.GoCell cell) {
            return new GoPointer(cell);
        }
        return new GoPointer(referent);
    }

    /** Fresh (unshared) zero value for new() allocations. */
    private static Object freshZero(GoKind kind, GoStructDescriptor structDesc,
            int[] arrayLens, GoKind arrayUltimateElemKind, GoStructDescriptor ultimateStructDesc) {
        // P3-M8b: new([N]T) allocates a Java array of N zero elements.
        // P3-M9: nested arrays ([N][M]T) allocate recursively — Go
        // zero-initializes the inner arrays; null elements would fault
        // on arr[i][j] access. Struct elements are zero-initialized too.
        if (kind == GoKind.ARRAY) {
            if (arrayLens == null || arrayLens.length == 0 || arrayUltimateElemKind == null) {
                throw new FrontendException("new([N]T) needs array lengths and ultimate element kind (WS E)");
            }
            return allocateNestedArray(arrayLens, 0, arrayUltimateElemKind, ultimateStructDesc);
        }
        // P3-M9 (ecdsa root fix): scalar new(T) allocates a fresh GoBox
        // cell holding the canonical zero (see executeObject) — the same
        // storage model as &x — instead of a single-element carrier array.
        return new graalgo.values.GoBox(Boxing.zero(kind, structDesc));
    }

    /** Recursively allocates a nested array: lens[depth] is the length at this level. */
    private static Object allocateNestedArray(int[] lens, int depth, GoKind elemKind,
            GoStructDescriptor ultimateStructDesc) {
        int len = lens[depth];
        if (depth == lens.length - 1) {
            // Innermost level: typed array of the ultimate element kind.
            return allocateArray(elemKind, len, ultimateStructDesc);
        }
        // Outer level: Object[] whose elements are recursively allocated.
        Object[] arr = new Object[len];
        for (int i = 0; i < len; i++) {
            arr[i] = allocateNestedArray(lens, depth + 1, elemKind, ultimateStructDesc);
        }
        return arr;
    }

    /** Allocates a Java array for new([N]T). Mirrors UnsafeOps.expectedArrayClass. */
    private static Object allocateArray(GoKind elemKind, int len, GoStructDescriptor ultimateStructDesc) {
        // P3-M9: new([N]S) zero-initializes struct elements (Go zero value),
        // not nulls — a nil element would fault on p.x access.
        if (elemKind == GoKind.STRUCT && ultimateStructDesc != null) {
            Object[] arr = new Object[len];
            for (int i = 0; i < len; i++) {
                arr[i] = Boxing.zero(GoKind.STRUCT, ultimateStructDesc);
            }
            return arr;
        }
        return switch (elemKind) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> new int[len];
            case INT64, UINT64, UNTYPED_INT -> new long[len];
            case FLOAT32 -> new float[len];
            case FLOAT64, UNTYPED_FLOAT -> new double[len];
            case BOOL, UNTYPED_BOOL -> new boolean[len];
            case COMPLEX64 -> new graalgo.values.GoComplex32[len];
            case COMPLEX128, UNTYPED_COMPLEX -> new graalgo.values.GoComplex64[len];
            default -> new Object[len];
        };
    }
}
