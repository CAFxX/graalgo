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

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc) {
        this(elemKind, structDesc, null, null, null);
    }

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc, int[] arrayLens, GoKind arrayUltimateElemKind) {
        this(elemKind, structDesc, arrayLens, arrayUltimateElemKind, null);
    }

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc,
            int[] arrayLens, GoKind arrayUltimateElemKind, GoStructDescriptor ultimateStructDesc) {
        super(GoKind.POINTER);
        this.elemKind = elemKind;
        this.structDesc = structDesc;
        this.arrayLens = arrayLens;
        this.arrayUltimateElemKind = arrayUltimateElemKind;
        this.ultimateStructDesc = ultimateStructDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // P3-M8b: each new() must allocate a FRESH referent object. Boxing.zero
        // returns cached shared values (e.g. Long.valueOf(0L)) — if two new(int)
        // calls share the same referent, unsafe.Pointer interning (identity-based)
        // would give them the same handle, breaking p1 != q1 for distinct allocations.
        Object referent = freshZero(elemKind, structDesc, arrayLens, arrayUltimateElemKind, ultimateStructDesc);
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
        return switch (kind.slotKind()) {
            case Int -> new int[1];
            case Long -> new long[1];
            case Float -> new float[1];
            case Double -> new double[1];
            case Boolean -> new boolean[1];
            case Object -> Boxing.zero(kind, structDesc);
            default -> throw new FrontendException("no zero for kind " + kind + " (WS E)");
        };
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
