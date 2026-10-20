package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Pointer dereference read {@code *p} (P3-M9, WS D).
 *
 * <p>Evaluates the pointer expression; a nil pointer raises gc's
 * nil-dereference panic. Otherwise returns the referent, following the
 * SelectorNode read pattern (Long-carried values decoded via
 * Boxing.fromLongBits; executeLong via Boxing.toLong for correct
 * unsigned extension).
 *
 * <p>P3-M16 (r16): unsafe interface reinterpretation. The canonical Go
 * idiom {@code *(*emptyInterface)(unsafe.Pointer(&i))} reads an interface
 * value's (type, data) words as a struct. graalgo represents interface
 * values as {@link GoInterface} objects, not word pairs, so dereferencing
 * a {@code *S} (S struct) whose referent is a GoInterface must materialize
 * S from the interface's (descriptor, data), mapped positionally to S's
 * fields. Without this, the raw GoInterface flows into struct field
 * selection and NPEs/CCEs (red-master runtime layer: reflect.TypeOf via
 * abi.EmptyInterface).
 */
public final class DerefNode extends GoExprNode {
    @Child private GoExprNode ptr;
    /** Struct descriptor when the dereferenced type is a struct; null otherwise. */
    private final GoStructDescriptor structDesc;

    public DerefNode(GoKind kind, GoExprNode ptr) {
        this(kind, ptr, null);
    }

    public DerefNode(GoKind kind, GoExprNode ptr, GoStructDescriptor structDesc) {
        super(kind);
        this.ptr = ptr;
        this.structDesc = structDesc;
    }

    private Object read(VirtualFrame frame) {
        Object o = ptr.executeObject(frame);
        if (o == null) {
            // Java null is the nil *T (e.g., var p *int).
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        if (o instanceof GoPointer p) {
            if (p.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            }
            o = p.referent();
        }
        if (o instanceof java.util.concurrent.atomic.AtomicReference<?> ar) {
            // PKG-053: dereferencing an atomic.Pointer/Value cell (&x.v
            // yields the cell itself — see FieldAddrNode) reads the
            // stored reference, e.g. LoadPointer(&x.v) in
            // (*atomic.Pointer[T]).Load.
            return ar.get();
        }
        if (o instanceof Long l) {
            // Long-carried value: decode per the node's kind.
            return Boxing.fromLongBits(kind(), l.longValue());
        }
        if (o instanceof GoInterface gi && structDesc != null) {
            // P3-M16: unsafe interface reinterpretation
            // (*(*S)(unsafe.Pointer(&i))). The referent is the interface
            // value itself; materialize S from its (descriptor, data).
            return materializeInterfaceAsStruct(gi, structDesc);
        }
        if (o == null && kind() == GoKind.UNSAFE_POINTER) {
            // Nil unsafe.Pointer referent: the zero value is address 0,
            // not Java null (cf. SelectorNode.read).
            return Long.valueOf(0);
        }
        return o;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // P3-M9: use Boxing.toLong for correct zero-extension of unsigned
        // kinds (uint32(0xFFFFFFFF) -> 4294967295L, not -1L).
        return Boxing.toLong(read(frame), kind());
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) read(frame)).intValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Float) read(frame)).floatValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Double) read(frame)).doubleValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return ((Boolean) read(frame)).booleanValue();
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return read(frame);
    }

    /**
     * Materializes a struct from an interface value's (descriptor, data)
     * pair (P3-M16). Implements the unsafe reinterpretation idiom
     * {@code *(*S)(unsafe.Pointer(&i))}: S's fields, in declaration order,
     * receive the interface's words positionally (field 0 ← type word,
     * field 1 ← data word; further fields, if any, are zero). Each word is
     * converted to the field's carrier:
     * <ul>
     *   <li>type word → POINTER-kind field: a {@link GoPointer} to the
     *       materialized {@code internal/abi.Type} struct (nil descriptor →
     *       nil pointer); UNSAFE_POINTER-kind field: interned handle (0 for
     *       nil).</li>
     *   <li>data word → UNSAFE_POINTER-kind field: interned handle (0 for
     *       nil); POINTER-kind field: pointer to the data; otherwise the
     *       word is stored as-is when the carrier matches.</li>
     * </ul>
     */
    public static GoStruct materializeInterfaceAsStruct(GoInterface gi, GoStructDescriptor desc) {
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        var fields = desc.fields();
        // The interface's two words, in order.
        Object[] words = {gi.descriptor(), gi.data()};
        for (int i = 0; i < fields.size() && i < words.length; i++) {
            var f = fields.get(i);
            Object word = words[i];
            if (f.inLongs()) {
                longs[f.index()] = toLongWord(word, f.kind(), i == 0);
            } else {
                objects[f.index()] = toObjectWord(gi, word, f.kind(), i == 0);
            }
        }
        return new GoStruct(desc, longs, objects);
    }

    /** Converts an interface word to a longs-carrier field value. */
    private static long toLongWord(Object word, GoKind kind, boolean isTypeWord) {
        if (word == null) {
            return 0L;
        }
        if (kind == GoKind.UNSAFE_POINTER) {
            if (word instanceof Long l) {
                return l;
            }
            // The word as an unsafe.Pointer address: intern for a handle.
            return graalgo.p3unsafe.UnsafeTable.current().intern(word);
        }
        return Boxing.toLong(word, kind);
    }

    /** Converts an interface word to an objects-carrier field value. */
    private static Object toObjectWord(GoInterface gi, Object word, GoKind kind, boolean isTypeWord) {
        if (word == null) {
            // Nil word: nil pointer for POINTER-kind fields.
            return kind == GoKind.POINTER ? new GoPointer(null) : null;
        }
        if (kind == GoKind.POINTER) {
            if (word instanceof GoPointer gp) {
                return gp;
            }
            if (isTypeWord) {
                // The type word as *abi.Type: materialize the abi.Type
                // struct from the interface's dynamic type. Real reflect
                // code reads struct fields (Kind_, Size_, ...) from it.
                return new GoPointer(materializeAbiType(gi));
            }
            // Data word as *T: pointer to the data value.
            return new GoPointer(word);
        }
        if (kind == GoKind.UNSAFE_POINTER) {
            // P3-M17: unsafe.Pointer fields carry interned handles (Long),
            // not the raw value. The data word as unsafe.Pointer is the
            // handle for the value's storage identity.
            if (word instanceof Long l) {
                return l;
            }
            return Long.valueOf(graalgo.p3unsafe.UnsafeTable.current().intern(word));
        }
        return word;
    }

    /**
     * Materializes the {@code internal/abi.Type} struct for an interface's
     * dynamic type (P3-M16). The struct layout matches gc's abi.Type:
     * Size_, PtrBytes, Hash, TFlag, Align_, FieldAlign_, Kind_, Equal,
     * GCData, Str, PtrToThis. Fields are populated from the type's
     * TypeInfo; function/pointer fields are nil.
     */
    private static GoStruct materializeAbiType(GoInterface gi) {
        long tok = graalgo.p3reflect.ReflectValues.typeof(gi);
        graalgo.p3reflect.ReflectTypes.TypeInfo ti =
                graalgo.p3reflect.ReflectTypes.table().info(tok);

        GoStructDescriptor desc = abiTypeDescriptor();
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        for (var f : desc.fields()) {
            if (f.inLongs()) {
                longs[f.index()] = switch (f.name()) {
                    case "Size_" -> ti.size;
                    case "PtrBytes" -> 0L;
                    case "Hash" -> 0L;
                    case "TFlag" -> 0L;
                    case "Align_" -> ti.align;
                    case "FieldAlign_" -> ti.align;
                    case "Kind_" -> abiKindValue(ti.kind);
                    case "Str" -> 0L;
                    case "PtrToThis" -> 0L;
                    default -> 0L;
                };
            } else {
                // Equal (func), GCData (*byte): nil.
                objects[f.index()] = null;
            }
        }
        return new GoStruct(desc, longs, objects);
    }

    /** Cached descriptor for internal/abi.Type. */
    private static volatile GoStructDescriptor abiTypeDesc;

    private static GoStructDescriptor abiTypeDescriptor() {
        GoStructDescriptor d = abiTypeDesc;
        if (d == null) {
            synchronized (DerefNode.class) {
                d = abiTypeDesc;
                if (d == null) {
                    d = GoStructDescriptor.layout(
                            "internal/abi.Type",
                            java.util.List.of("Size_", "PtrBytes", "Hash", "TFlag",
                                    "Align_", "FieldAlign_", "Kind_", "Equal",
                                    "GCData", "Str", "PtrToThis"),
                            java.util.List.of(
                                    GoKind.UINT64, GoKind.UINT64, GoKind.UINT32,
                                    GoKind.UINT8, GoKind.UINT8, GoKind.UINT8,
                                    GoKind.UINT8, GoKind.FUNC, GoKind.POINTER,
                                    GoKind.INT32, GoKind.INT32));
                    abiTypeDesc = d;
                }
            }
        }
        return d;
    }

    /** Maps GoKind to the numeric internal/abi.Kind value (gc-exact). */
    private static int abiKindValue(GoKind kind) {
        return switch (kind) {
            case BOOL -> 1;
            case INT8 -> 3;
            case INT16 -> 4;
            case INT32 -> 5;
            case INT64 -> 6;  // int, int64
            case UINT8 -> 8;
            case UINT16 -> 9;
            case UINT32 -> 10;
            case UINT64 -> 11;  // uint, uintptr
            case FLOAT32 -> 13;
            case FLOAT64 -> 14;
            case COMPLEX64 -> 15;
            case COMPLEX128 -> 16;
            case ARRAY -> 17;
            case CHAN -> 18;
            case FUNC -> 19;
            case INTERFACE -> 20;
            case MAP -> 21;
            case POINTER -> 22;
            case SLICE -> 23;
            case STRING -> 24;
            case STRUCT -> 25;
            case UNSAFE_POINTER -> 26;
            default -> 0;
        };
    }
}
