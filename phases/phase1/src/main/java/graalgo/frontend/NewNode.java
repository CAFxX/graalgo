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
    private final int arrayLen; // >=0 iff elemKind == ARRAY
    private final GoKind arrayElemKind; // non-null iff elemKind == ARRAY

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc) {
        this(elemKind, structDesc, -1, null);
    }

    public NewNode(GoKind elemKind, GoStructDescriptor structDesc, int arrayLen, GoKind arrayElemKind) {
        super(GoKind.POINTER);
        this.elemKind = elemKind;
        this.structDesc = structDesc;
        this.arrayLen = arrayLen;
        this.arrayElemKind = arrayElemKind;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // P3-M8b: each new() must allocate a FRESH referent object. Boxing.zero
        // returns cached shared values (e.g. Long.valueOf(0L)) — if two new(int)
        // calls share the same referent, unsafe.Pointer interning (identity-based)
        // would give them the same handle, breaking p1 != q1 for distinct allocations.
        Object referent = freshZero(elemKind, structDesc, arrayLen, arrayElemKind);
        return new GoPointer(referent);
    }

    /** Fresh (unshared) zero value for new() allocations. */
    private static Object freshZero(GoKind kind, GoStructDescriptor structDesc, int arrayLen, GoKind arrayElemKind) {
        // P3-M8b: new([N]T) allocates a Java array of N zero elements.
        if (kind == GoKind.ARRAY) {
            if (arrayLen < 0 || arrayElemKind == null) {
                throw new FrontendException("new([N]T) needs array length and element kind (WS E)");
            }
            return allocateArray(arrayElemKind, arrayLen);
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

    /** Allocates a Java array for new([N]T). Mirrors UnsafeOps.expectedArrayClass. */
    private static Object allocateArray(GoKind elemKind, int len) {
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
