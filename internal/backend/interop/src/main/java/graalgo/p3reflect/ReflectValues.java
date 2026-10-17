package graalgo.p3reflect;

import graalgo.p3reflect.ReflectRefs.Cell;
import graalgo.p3reflect.ReflectTypes.FieldInfo;
import graalgo.p3reflect.ReflectTypes.Table;
import graalgo.p3reflect.ReflectTypes.TypeInfo;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.ArrayList;
import java.util.List;

/**
 * Value packing, getters, setters, and conversion for the reflect package
 * (P3-M8, wave 2).
 *
 * <p>A synthetic {@code reflect.Value} is a {@link GoStruct} of three
 * fields: {@code typ} and {@code ptr} (both {@code unsafe.Pointer}, in the
 * objects carrier as boxed {@code Long} handles) and {@code flag}
 * ({@code uintptr}, in the longs carrier). The zero Value has
 * {@code typ == 0}.
 *
 * <p>Flag bits mirror gc exactly: {@code flagStickyRO=1<<5},
 * {@code flagEmbedRO=1<<6}, {@code flagIndir=1<<7},
 * {@code flagAddr=1<<8}, {@code flagRO=flagStickyRO|flagEmbedRO}.
 */
public final class ReflectValues {
    private ReflectValues() {}

    // Flag bits (gc-exact).
    public static final long FLAG_STICKY_RO = 1L << 5;
    public static final long FLAG_EMBED_RO = 1L << 6;
    // gc's flagIndir: set by Elem on the result (both interface-unwrap and
    // pointer-deref branches). Invisible to CanAddr/CanSet/CanInterface
    // (they only read flagAddr/flagRO); consumed by the Cell-handle
    // provenance rule in ReflectOpNode.reflectElem.
    public static final long FLAG_INDIR = 1L << 7;
    public static final long FLAG_ADDR = 1L << 8;
    public static final long FLAG_RO = FLAG_STICKY_RO | FLAG_EMBED_RO;

    /** Unpacks a Value struct into (typ, ptr, flag). */
    public record ValueParts(long typ, long ptr, long flag) {}

    public static ValueParts parts(GoStruct v) {
        Object typObj = v.getObject(0);
        Object ptrObj = v.getObject(1);
        long typ = typObj instanceof Long l ? l : 0;
        long ptr = ptrObj instanceof Long l ? l : 0;
        // flag is field index 2 (typ/ptr are unsafe.Pointer, objects-carried
        // per GoStructDescriptor.layout; only flag is longs-carried).
        // GoStruct accessors take FIELD indices, not carrier indices.
        return new ValueParts(typ, ptr, v.getLong(2));
    }

    public static boolean isValid(GoStruct v) {
        return parts(v).typ() != 0;
    }

    /**
     * Packs a Value struct. The descriptor is built from the wire at node
     * build time (see ReflectOpNode) and matches NodeFactory's layout
     * exactly (same field order, same {@code layout()} assignment).
     */
    public static GoStruct pack(GoStructDescriptor desc, long typ, long ptr, long flag) {
        return new GoStruct(desc, new long[]{flag}, new Object[]{typ, ptr});
    }

    /** The zero Value. */
    public static GoStruct zero(GoStructDescriptor desc) {
        return pack(desc, 0, 0, 0);
    }

    // ------------------------------------------------------------------
    // Constructors.
    // ------------------------------------------------------------------

    /**
     * {@code __reflect_typeof}: token for the dynamic type of an
     * interface value. Returns 0 for nil.
     *
     * <p>The interface's {@link GoTypeDescriptor} maps to a wire id via
     * the staged NodeFactory registry (patch 03), then to the canonical
     * token via the install-time wireId→tok map.
     */
    public static long typeof(Object iface) {
        if (iface == null || iface == graalgo.values.GoInterface.NIL) {
            return 0;
        }
        Object desc;
        if (iface instanceof graalgo.values.GoInterface gi) {
            if (gi.isNil()) {
                return 0;
            }
            desc = gi.descriptor();
        } else {
            // Direct value (should not happen for 'any', but be safe).
            return tokForDirectValue(iface);
        }
        return tokForDescriptor(desc);
    }

    /**
     * Token for a GoTypeDescriptor via the patch-03 registry. Called from
     * ReflectTypes (which holds the wireId→tok map).
     */
    public static long tokForDescriptor(Object descriptor) {
        return ReflectTypes.tokForDescriptor(descriptor);
    }

    /** Structural token for a direct D5 value (non-interface path). */
    private static long tokForDirectValue(Object v) {
        ReflectTypes.Table table = ReflectTypes.table();
        GoKind k = kindOfValue(v);
        return table.tokForKind(k);
    }

    /** Best-effort GoKind for a direct D5 value. */
    private static GoKind kindOfValue(Object v) {
        if (v instanceof Integer) {
            return GoKind.INT32;
        }
        if (v instanceof Long) {
            return GoKind.INT64;
        }
        if (v instanceof Double) {
            return GoKind.FLOAT64;
        }
        if (v instanceof Float) {
            return GoKind.FLOAT32;
        }
        if (v instanceof Boolean) {
            return GoKind.BOOL;
        }
        if (v instanceof GoString) {
            return GoKind.STRING;
        }
        if (v instanceof GoStruct) {
            return GoKind.STRUCT;
        }
        if (v instanceof GoSlice) {
            return GoKind.SLICE;
        }
        if (v instanceof GoMap) {
            return GoKind.MAP;
        }
        if (v instanceof GoPointer) {
            return GoKind.POINTER;
        }
        if (v != null && v.getClass().isArray()) {
            return GoKind.ARRAY;
        }
        throw new IllegalStateException("reflect: cannot infer kind of " + v);
    }

    /** Unboxes a Value struct to its D5 value (no RO check; synthetic Go checks). */
    public static Object unbox(GoStruct v) {
        ValueParts p = parts(v);
        Object d5 = ReflectRefs.load(p.ptr());
        // RV-P1-23 §6f: keep v reachable past the last handle-derived use.
        java.lang.ref.Reference.reachabilityFence(v);
        return d5;
    }

    // ------------------------------------------------------------------
    // Scalar getters (canonical boxed forms).
    // ------------------------------------------------------------------

    public static long getInt(Object d5) {
        return ((Long) d5).longValue();
    }

    public static long getUint(Object d5) {
        if (d5 instanceof Long l) {
            return l.longValue();
        }
        return ((Integer) d5).intValue() & 0xFFFFFFFFL;
    }

    public static double getFloat(Object d5) {
        if (d5 instanceof Double dd) {
            return dd;
        }
        return ((Float) d5).floatValue();
    }

    // ------------------------------------------------------------------
    // Array helpers (D5 mapping, mirroring Boxing).
    // ------------------------------------------------------------------

    public static Object newArray(GoKind elemKind, int len) {
        // Single canonical allocator: Boxing.newArray owns the kind→backing
        // mapping (D5). This wrapper exists for call-site continuity only.
        return graalgo.frontend.Boxing.newArray(elemKind, len);
    }

    public static Object arrayGet(Object array, GoKind elemKind, int i) {
        return switch (elemKind) {
            // Narrow backing stores (D5): sign-extend on load, zero-extend
            // for unsigned kinds. Mirrors Boxing.arrayGet.
            case INT8 -> Integer.valueOf(((byte[]) array)[i]);
            case UINT8 -> Integer.valueOf(((byte[]) array)[i] & 0xFF);
            case INT16 -> Integer.valueOf(((short[]) array)[i]);
            case UINT16 -> Integer.valueOf(((short[]) array)[i] & 0xFFFF);
            case INT32, UINT32 -> Integer.valueOf(((int[]) array)[i]);
            case INT64, UINT64 -> Long.valueOf(((long[]) array)[i]);
            case FLOAT32 -> Float.valueOf(((float[]) array)[i]);
            case FLOAT64 -> Double.valueOf(((double[]) array)[i]);
            case BOOL -> Boolean.valueOf(((boolean[]) array)[i]);
            default -> ((Object[]) array)[i];
        };
    }

    public static void arraySet(Object array, GoKind elemKind, int i, Object boxed) {
        switch (elemKind) {
            // Narrowing conversion preserves the low bits; reads re-extend
            // per signedness. Mirrors Boxing.arraySet.
            case INT8, UINT8 -> ((byte[]) array)[i] = ((Integer) boxed).byteValue();
            case INT16, UINT16 -> ((short[]) array)[i] = ((Integer) boxed).shortValue();
            case INT32, UINT32 -> ((int[]) array)[i] = ((Integer) boxed).intValue();
            case INT64, UINT64 -> ((long[]) array)[i] = ((Long) boxed).longValue();
            case FLOAT32 -> ((float[]) array)[i] = ((Float) boxed).floatValue();
            case FLOAT64 -> ((double[]) array)[i] = ((Double) boxed).doubleValue();
            case BOOL -> ((boolean[]) array)[i] = ((Boolean) boxed).booleanValue();
            default -> ((Object[]) array)[i] = boxed;
        }
    }

    // ------------------------------------------------------------------
    // Struct field access (via GoStructDescriptor.layout positions).
    // ------------------------------------------------------------------

    /**
     * Reads a struct field in canonical boxed form. The field's carrier
     * (longs vs objects) is determined by its GoKind, mirroring
     * GoStructDescriptor.layout.
     */
    public static Object structFieldGet(GoStruct s, int fieldIndex, GoKind kind) {
        // The descriptor's Field records carry the carrier assignment;
        // use them directly (robust against layout changes).
        GoStructDescriptor.Field f = s.descriptor().field(fieldIndex);
        if (f.inLongs()) {
            // GoStruct accessors take FIELD indices (they translate to
            // carrier indices internally); passing f.index() here would
            // double-translate.
            long bits = s.getLong(fieldIndex);
            return switch (kind) {
                case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> Integer.valueOf((int) bits);
                case INT64, UINT64 -> Long.valueOf(bits);
                case FLOAT32 -> Float.valueOf(Float.intBitsToFloat((int) bits));
                case FLOAT64 -> Double.valueOf(Double.longBitsToDouble(bits));
                case BOOL -> Boolean.valueOf(bits != 0);
                default -> throw new IllegalStateException("reflect: field in longs with kind " + kind);
            };
        }
        return s.getObject(fieldIndex);
    }

    public static void structFieldSet(GoStruct s, int fieldIndex, GoKind kind, Object boxed) {
        GoStructDescriptor.Field f = s.descriptor().field(fieldIndex);
        if (f.inLongs()) {
            long bits = switch (kind) {
                case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> ((Integer) boxed).intValue();
                case INT64, UINT64 -> ((Long) boxed).longValue();
                case FLOAT32 -> Float.floatToIntBits(((Float) boxed).floatValue());
                case FLOAT64 -> Double.doubleToLongBits(((Double) boxed).doubleValue());
                case BOOL -> ((Boolean) boxed) ? 1L : 0L;
                default -> throw new IllegalStateException("reflect: field in longs with kind " + kind);
            };
            s.setLong(fieldIndex, bits);
        } else {
            s.setObject(fieldIndex, boxed);
        }
    }
}
