package graalgo.values;

import com.oracle.truffle.api.frame.FrameSlotKind;

/**
 * The Go value kinds relevant to Phase 1, per PLAN.md D5 (value-model Java
 * mapping).
 *
 * <p>Mapping: int8/16/32 + uint8/16/32 → {@code int}; int64/uint64/int/uint/
 * uintptr → {@code long}; float32 → {@code float}; float64 → {@code double};
 * bool → {@code boolean}; string → {@link GoString}; slice → {@link GoSlice};
 * array → typed Java array; struct → {@link GoStruct}; map → {@link GoMap};
 * interface → {@link GoInterface}; func → {@link GoFunc}; pointer → opaque
 * Java reference; unsafe.Pointer → handle-table entry ({@link UnsafeHandles}).
 *
 * <p>Untyped kinds ({@code UNTYPED_*}) arise only from constants. They are
 * <em>transparent</em>: a constant node built at an untyped kind implements
 * the execute methods of every type in its family, and parents call the
 * method for <em>their</em> kind directly — no conversion node is inserted
 * (mirrors go/types, where untyped constants assume the context type).
 */
public enum GoKind {
    INT8(true, 8),
    INT16(true, 16),
    INT32(true, 32),
    INT64(true, 64),
    UINT8(false, 8),
    UINT16(false, 16),
    UINT32(false, 32),
    UINT64(false, 64),
    FLOAT32(false, 32),
    FLOAT64(false, 64),
    COMPLEX64(false, 64),
    COMPLEX128(false, 128),
    BOOL(false, 1),
    STRING(false, 0),
    UNTYPED_INT(true, 64),
    UNTYPED_FLOAT(false, 64),
    UNTYPED_COMPLEX(false, 128),
    UNTYPED_STRING(false, 0),
    UNTYPED_BOOL(false, 1),
    SLICE(false, 0),
    ARRAY(false, 0),
    MAP(false, 0),
    STRUCT(false, 0),
    POINTER(false, 0),
    INTERFACE(false, 0),
    FUNC(false, 0),
    CHAN(false, 0),
    UNSAFE_POINTER(false, 0),
    /**
     * A generic type parameter (e.g. {@code T} in {@code func F[T any](x T)}).
     * Type parameters appear in the type table when generic declarations are
     * exported (the frontend emits them for unsubstituted signatures). At
     * runtime they behave like their constraint: the method set and layout
     * come from the constraint type. Instantiated uses are substituted with
     * concrete types before reaching the backend.
     */
    TYPEPARAM(false, 0);

    private final boolean signed;
    private final int bits;

    GoKind(boolean signed, int bits) {
        this.signed = signed;
        this.bits = bits;
    }

    /** True for signed integer kinds (int8..int64 and untyped int). */
    public boolean isSigned() {
        return signed;
    }

    /** Bit width for integer/float kinds; 0 for aggregate/reference kinds. */
    public int bits() {
        return bits;
    }

    public boolean isIntKind() {
        return switch (this) {
            case INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64, UNTYPED_INT -> true;
            default -> false;
        };
    }

    public boolean isUintKind() {
        return switch (this) {
            case UINT8, UINT16, UINT32, UINT64 -> true;
            default -> false;
        };
    }

    public boolean isFloatKind() {
        return this == FLOAT32 || this == FLOAT64 || this == UNTYPED_FLOAT;
    }

    /** True for complex64/complex128 and untyped complex (WS G). */
    public boolean isComplexKind() {
        return this == COMPLEX64 || this == COMPLEX128 || this == UNTYPED_COMPLEX;
    }

    /** The untyped kind of the same family, or null if this kind is typed. */
    public boolean isUntyped() {
        return switch (this) {
            case UNTYPED_INT, UNTYPED_FLOAT, UNTYPED_COMPLEX, UNTYPED_STRING, UNTYPED_BOOL -> true;
            default -> false;
        };
    }

    /**
     * True when a value built at {@code from} may be used directly where
     * {@code to} is expected, without a conversion node (untyped
     * transparency: constants assume the context type, mirroring go/types).
     * Untyped ints are also transparent to float kinds (the constant is
     * re-rounded from its exact decimal); every other cross-kind use needs
     * a real conversion.
     */
    public static boolean transparent(GoKind from, GoKind to) {
        if (from == to) {
            return true;
        }
        return switch (from) {
            case UNTYPED_INT -> to.isIntKind() || to.isFloatKind();
            case UNTYPED_FLOAT -> to == FLOAT32 || to == FLOAT64;
            case UNTYPED_COMPLEX -> to.isComplexKind();
            case UNTYPED_STRING -> to == STRING;
            case UNTYPED_BOOL -> to == BOOL;
            default -> false;
        };
    }

    /** Frame slot kind for a variable of this Go kind (D5 mapping). */
    public FrameSlotKind slotKind() {
        return switch (this) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> FrameSlotKind.Int;
            // P3-M8: unsafe.Pointer values are long handles (D5), not objects.
            case INT64, UINT64, UNTYPED_INT, UNSAFE_POINTER -> FrameSlotKind.Long;
            case FLOAT32 -> FrameSlotKind.Float;
            case FLOAT64, UNTYPED_FLOAT -> FrameSlotKind.Double;
            case BOOL, UNTYPED_BOOL -> FrameSlotKind.Boolean;
            default -> FrameSlotKind.Object;
        };
    }

    /**
     * Maps a frontend/v1 type-table {@code basic} entry to a GoKind.
     * {@code name} is the go/types basic name (e.g. "int32", "uint64",
     * "float32", "bool", "string", "untyped int"); {@code cls} is the
     * class ("int", "uint", "float", "bool", "string", "complex",
     * "pointer", "untyped", "unknown").
     *
     * @throws IllegalArgumentException for classes with no Phase-1 mapping —
     *         the caller turns this into a workstream-attributed factory
     *         error. (Complex kinds mapped since WS G.)
     */
    public static GoKind fromBasic(String name, String cls) {
        return switch (cls) {
            case "int" -> switch (name) {
                case "int8" -> INT8;
                case "int16" -> INT16;
                case "int32", "rune" -> INT32;
                case "int64", "int" -> INT64;
                case "untyped int", "untyped rune" -> UNTYPED_INT;
                default -> throw new IllegalArgumentException("unknown int type name \"" + name + "\"");
            };
            case "uint" -> switch (name) {
                case "uint8", "byte" -> UINT8;
                case "uint16" -> UINT16;
                case "uint32" -> UINT32;
                case "uint64", "uint", "uintptr" -> UINT64;
                case "untyped int", "untyped rune" -> UNTYPED_INT;
                default -> throw new IllegalArgumentException("unknown uint type name \"" + name + "\"");
            };
            case "float" -> switch (name) {
                case "float32" -> FLOAT32;
                case "float64" -> FLOAT64;
                case "untyped float" -> UNTYPED_FLOAT;
                default -> throw new IllegalArgumentException("unknown float type name \"" + name + "\"");
            };
            case "complex" -> switch (name) {
                case "complex64" -> COMPLEX64;
                case "complex128" -> COMPLEX128;
                case "untyped complex" -> UNTYPED_COMPLEX;
                default -> throw new IllegalArgumentException("unknown complex type name \"" + name + "\"");
            };
            case "bool" -> switch (name) {
                case "bool" -> BOOL;
                case "untyped bool" -> UNTYPED_BOOL;
                default -> throw new IllegalArgumentException("unknown bool type name \"" + name + "\"");
            };
            case "string" -> switch (name) {
                case "string" -> STRING;
                case "untyped string" -> UNTYPED_STRING;
                default -> throw new IllegalArgumentException("unknown string type name \"" + name + "\"");
            };
            case "pointer" -> UNSAFE_POINTER; // only unsafe.Pointer has basic class "pointer"
            default -> throw new IllegalArgumentException(
                    "no Phase-1 value mapping for basic class \"" + cls + "\" (name \"" + name + "\")");
        };
    }

    /** Maps a frontend/v1 composite type-table kind to a GoKind. */
    public static GoKind fromCompositeKind(String kind) {
        return switch (kind) {
            case "slice" -> SLICE;
            case "array" -> ARRAY;
            case "map" -> MAP;
            case "struct" -> STRUCT;
            case "pointer" -> POINTER;
            case "interface" -> INTERFACE;
            case "signature" -> FUNC;
            case "chan" -> CHAN;
            default -> throw new IllegalArgumentException("unknown composite type kind \"" + kind + "\"");
        };
    }
}
