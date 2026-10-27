package graalgo.p3reflect;

import graalgo.frontend.Boxing;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.UnsafeHandles;
import graalgo.values.GoBox;
import graalgo.values.GoCell;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.lang.reflect.Array;
import java.util.ArrayList;
import java.util.List;

/**
 * The runtime-provided memory primitives of package reflect
 * (fix-redmaster-r28, Fix 4).
 *
 * <p>In gc, {@code reflect.unsafe_New} and {@code reflect.typedmemmove}
 * are declared bodyless in reflect ("implemented in package runtime")
 * and implemented in assembly. Graalgo resolves them through the
 * {@code UnimplementedRootNode} fallback: the launcher registers them
 * here under {@code reflect.unsafe_New} / {@code reflect.typedmemmove},
 * and the stub dispatches to the intrinsic instead of panicking.
 *
 * <p>Semantics (gc-exact for the carriers graalgo represents):
 * <ul>
 *   <li>{@code unsafe_New(t)} allocates a zero value of type {@code t}
 *       and returns an {@code unsafe.Pointer} to it — in graalgo's
 *       model, a {@link UnsafeTable} handle to a {@link GoBox} cell
 *       holding the zero carrier, so later writes through the handle
 *       are observable through every alias (gc aliasing).</li>
 *   <li>{@code typedmemmove(t, dst, src)} copies the value of type
 *       {@code t} from {@code src} to {@code dst} — gc's byte copy:
 *       structs and arrays are copied by value (nested structs/arrays
 *       recursively), slices copy the header (the backing array stays
 *       shared), and reference values (pointers, maps, chans, funcs,
 *       interfaces, strings) copy the reference.</li>
 * </ul>
 *
 * <p>Fail-closed: a type whose token cannot be recovered from the
 * materialized {@code abi.Type} struct, a handle that does not resolve,
 * or a carrier shape the copy does not understand throws
 * {@link UnsafeHandles.UnsafeEscapeException} — a loud fatal, never a
 * silently wrong value. A wrong data word silently corrupts every
 * interface value built from it (reflect's packEface), while a trap
 * names the missing case.
 */
public final class ReflectMem {
    private ReflectMem() {}

    /** {@code reflect.unsafe_New(t *abi.Type) unsafe.Pointer}. */
    public static Object unsafeNew(Object typeArg) {
        ReflectTypes.TypeInfo ti = typeInfoOf(typeArg);
        GoBox cell = new GoBox(zeroOf(ti));
        return Long.valueOf(UnsafeTable.current().intern(cell));
    }

    /** {@code reflect.typedmemmove(t *abi.Type, dst, src unsafe.Pointer)}. */
    public static Object typedmemmove(Object typeArg, Object dstArg, Object srcArg) {
        ReflectTypes.TypeInfo ti = typeInfoOf(typeArg);
        Object srcValue = readPtr(srcArg);
        writePtr(dstArg, ti, copyOf(ti, srcValue));
        return null;
    }

    /**
     * Recovers the {@link ReflectTypes.TypeInfo} for a {@code *abi.Type}
     * argument: the D5 form is a {@link GoPointer} to a materialized
     * {@code internal/abi.Type} struct (see {@code DerefNode}'s
     * {@code materializeAbiTypeForTok}, which registers the struct →
     * token mapping this reads). A bare struct or a handle to one is
     * accepted too; anything else fails closed.
     */
    static ReflectTypes.TypeInfo typeInfoOf(Object typeArg) {
        Object raw = typeArg;
        if (raw instanceof GoPointer p) {
            raw = p.referent();
        } else if (raw instanceof Long h) {
            raw = UnsafeTable.current().resolve(h.longValue());
            if (raw instanceof GoPointer p) {
                raw = p.referent();
            }
        }
        if (!(raw instanceof GoStruct gs)) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "reflect memory primitive: *abi.Type argument is not a materialized abi.Type struct: "
                            + (raw == null ? "null" : raw.getClass().getSimpleName()));
        }
        ReflectTypes.Table table = ReflectTypes.table();
        long tok = table.abiTypeTok(gs);
        if (tok <= 0) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "reflect memory primitive: abi.Type struct has no registered type token");
        }
        return table.info(tok);
    }

    /** The Go zero value of a type, in its canonical carrier. */
    static Object zeroOf(ReflectTypes.TypeInfo ti) {
        return switch (ti.kind) {
            case BOOL, UNTYPED_BOOL -> Boolean.FALSE;
            case INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64,
                    UNTYPED_INT -> Long.valueOf(0);
            case FLOAT32 -> Float.valueOf(0f);
            case FLOAT64, UNTYPED_FLOAT -> Double.valueOf(0d);
            case COMPLEX64 -> GoComplex32.ZERO;
            case COMPLEX128, UNTYPED_COMPLEX -> GoComplex64.ZERO;
            case STRING, UNTYPED_STRING -> GoString.EMPTY;
            case SLICE -> null; // nil slice is the null GoSlice reference
            case ARRAY -> Boxing.newArray(ti.elem.kind,
                    ti.elem.kind == GoKind.STRUCT ? structDescriptorOf(ti.elem) : null,
                    ti.arrayLen);
            case STRUCT -> GoStruct.zero(structDescriptorOf(ti));
            // Reference types: the zero value is nil.
            case POINTER, MAP, CHAN, FUNC, INTERFACE, UNSAFE_POINTER -> null;
            default -> throw new UnsafeHandles.UnsafeEscapeException(
                    "reflect memory primitive: no zero value for kind " + ti.kind);
        };
    }

    /**
     * Builds a {@link GoStructDescriptor} for a struct type from its
     * {@link ReflectTypes.TypeInfo} fields (recursing into nested
     * struct fields; recursion terminates because Go struct types cannot
     * directly contain themselves, and pointer fields stop the walk).
     */
    static GoStructDescriptor structDescriptorOf(ReflectTypes.TypeInfo ti) {
        List<String> names = new ArrayList<>();
        List<GoKind> kinds = new ArrayList<>();
        List<Integer> arrayLens = new ArrayList<>();
        List<GoKind> arrayElemKinds = new ArrayList<>();
        List<GoStructDescriptor> arrayElemDescs = new ArrayList<>();
        List<GoStructDescriptor> structDescs = new ArrayList<>();
        for (ReflectTypes.FieldInfo f : ti.fields) {
            names.add(f.name);
            kinds.add(f.type.kind);
            if (f.type.kind == GoKind.ARRAY) {
                arrayLens.add(f.type.arrayLen);
                arrayElemKinds.add(f.type.elem.kind);
                arrayElemDescs.add(f.type.elem.kind == GoKind.STRUCT
                        ? structDescriptorOf(f.type.elem) : null);
            } else {
                arrayLens.add(-1);
                arrayElemKinds.add(null);
                arrayElemDescs.add(null);
            }
            structDescs.add(f.type.kind == GoKind.STRUCT ? structDescriptorOf(f.type) : null);
        }
        return GoStructDescriptor.layout(
                ti.name.isEmpty() ? "struct" : ti.name,
                names, kinds, arrayLens, arrayElemKinds, arrayElemDescs, structDescs);
    }

    /**
     * Reads the value at a pointer, in canonical boxed form: a handle
     * resolves through the {@link UnsafeTable} (a cell yields its current
     * contents, an element reference yields the array slot), a
     * {@link GoPointer} yields its referent (cells and element pointers
     * included), and a bare value passes through.
     */
    static Object readPtr(Object ptrArg) {
        Object raw = ptrArg;
        if (raw instanceof Long h) {
            raw = UnsafeTable.current().resolve(h.longValue());
        }
        if (raw instanceof GoPointer p) {
            raw = p.referent();
        }
        if (raw instanceof GoCell cell) {
            return cell.get();
        }
        if (raw instanceof UnsafeTable.ArrayRef ref) {
            return Array.get(ref.array(), ref.offset());
        }
        if (raw == null) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "reflect memory primitive: read through a nil pointer");
        }
        return raw;
    }

    /**
     * Writes a value through a pointer: a handle to a cell stores into
     * the cell (gc aliasing: every alias of the cell observes the
     * write), a handle to an element reference stores into the array
     * slot (with the carrier narrowing of {@code Boxing}'s element
     * stores), a {@link GoPointer} writes through its cell/element
     * storage, and a struct referent is copied field-wise. Anything
     * else fails closed (a value object itself cannot be written
     * through — there is no storage behind it).
     */
    static void writePtr(Object ptrArg, ReflectTypes.TypeInfo ti, Object value) {
        Object raw = ptrArg;
        if (raw instanceof Long h) {
            raw = UnsafeTable.current().resolve(h.longValue());
        }
        if (raw instanceof GoPointer p) {
            if (p.isCellPointer()) {
                p.cell().set(value);
                return;
            }
            if (p.isElemPointer()) {
                setArrayElement(p.elemArray(), ti.kind, p.elemIndex(), value);
                return;
            }
            if (p.isNil()) {
                throw new UnsafeHandles.UnsafeEscapeException(
                        "reflect memory primitive: write through a nil pointer");
            }
            raw = p.referent();
        }
        if (raw instanceof GoCell cell) {
            cell.set(value);
            return;
        }
        if (raw instanceof UnsafeTable.ArrayRef ref) {
            setArrayElement(ref.array(), ti.kind, ref.offset(), value);
            return;
        }
        if (raw instanceof GoStruct dst && value instanceof GoStruct src) {
            copyStructFields(dst, src);
            return;
        }
        throw new UnsafeHandles.UnsafeEscapeException(
                "reflect memory primitive: cannot write through "
                        + (raw == null ? "null" : raw.getClass().getSimpleName()));
    }

    /** Copies struct fields from src into dst (same layout required). */
    private static void copyStructFields(GoStruct dst, GoStruct src) {
        GoStructDescriptor d = dst.descriptor();
        GoStructDescriptor s = src.descriptor();
        if (d.longsCount() != s.longsCount() || d.objectsCount() != s.objectsCount()) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "reflect memory primitive: struct copy across different layouts");
        }
        for (int i = 0; i < d.fields().size(); i++) {
            GoStructDescriptor.Field f = d.field(i);
            if (f.inLongs()) {
                dst.setLong(i, src.getLong(i));
            } else {
                dst.setObject(i, src.getObject(i));
            }
        }
    }

    /**
     * The gc byte copy of a value, in canonical carriers: scalars and
     * references pass through; slices copy the header (backing array
     * shared); structs copy by value ({@code deepCopy}: nested structs
     * and arrays copied, references shared); arrays copy element-wise.
     */
    static Object copyOf(ReflectTypes.TypeInfo ti, Object value) {
        if (value == null) {
            return null;
        }
        return switch (ti.kind) {
            case STRUCT -> value instanceof GoStruct gs ? gs.deepCopy() : failCopy(ti, value);
            case ARRAY -> copyArray(ti, value);
            case SLICE -> value instanceof GoSlice gs
                    ? new GoSlice(gs.array(), gs.offset(), gs.len(), gs.cap())
                    : failCopy(ti, value);
            default -> value;
        };
    }

    private static Object failCopy(ReflectTypes.TypeInfo ti, Object value) {
        throw new UnsafeHandles.UnsafeEscapeException(
                "reflect memory primitive: cannot copy kind " + ti.kind + " from "
                        + value.getClass().getSimpleName());
    }

    /** Element-wise array copy (struct elements by value, the rest by reference). */
    private static Object copyArray(ReflectTypes.TypeInfo ti, Object value) {
        int len = Array.getLength(value);
        Object dst;
        if (value instanceof Object[] src) {
            dst = src.clone();
            if (ti.elem.kind == GoKind.STRUCT) {
                Object[] d = (Object[]) dst;
                for (int i = 0; i < len; i++) {
                    if (d[i] instanceof GoStruct gs) {
                        d[i] = gs.deepCopy();
                    }
                }
            }
            return dst;
        }
        // Primitive carrier arrays: clone preserves the carrier exactly.
        dst = Array.newInstance(value.getClass().getComponentType(), len);
        System.arraycopy(value, 0, dst, 0, len);
        return dst;
    }

    /** Stores one element with carrier narrowing (mirrors Boxing's element stores). */
    private static void setArrayElement(Object array, GoKind elemKind, int i, Object boxed) {
        switch (elemKind) {
            case INT8, UINT8 -> ((byte[]) array)[i] = ((Number) boxed).byteValue();
            case INT16, UINT16 -> ((short[]) array)[i] = ((Number) boxed).shortValue();
            case INT32, UINT32 -> ((int[]) array)[i] = ((Number) boxed).intValue();
            case INT64, UINT64, UNTYPED_INT -> ((long[]) array)[i] = ((Number) boxed).longValue();
            case FLOAT32 -> ((float[]) array)[i] = ((Number) boxed).floatValue();
            case FLOAT64, UNTYPED_FLOAT -> ((double[]) array)[i] = ((Number) boxed).doubleValue();
            case BOOL, UNTYPED_BOOL -> ((boolean[]) array)[i] = ((Boolean) boxed).booleanValue();
            default -> ((Object[]) array)[i] = boxed;
        }
    }
}
