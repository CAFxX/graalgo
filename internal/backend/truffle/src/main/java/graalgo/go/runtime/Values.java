package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import graalgo.go.types.GoType;
import graalgo.go.types.GoType.Field;
import graalgo.go.values.Complex;
import graalgo.go.values.Flat;
import graalgo.go.values.Iface;
import graalgo.go.values.Ptr;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.runtime.GoFloatFmt;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;

/**
 * Type-directed operations on values in carrier form: generic load/store of a slot location,
 * Go's {@code ==} and hashing (interface values, map keys of non-integer kinds), and the
 * formatting of {@code print}/{@code println}. All of it is slow-path code: statically typed
 * sites use dedicated nodes and reach this class only where the type is dynamic.
 */
public final class Values {
    private Values() {}

    /** Thrown inside comparisons and hashing; the calling node turns it into the Go panic. */
    public static final class Uncomparable extends RuntimeException {
        private static final long serialVersionUID = 1L;
        public final transient GoType type;
        public final boolean hashing;

        Uncomparable(GoType type, boolean hashing) {
            super(null, null, false, false);
            this.type = type;
            this.hashing = hashing;
        }
    }

    /** The value of type {@code t} at a location, boxed in carrier form (a copy for flat types). */
    public static Object load(GoType t, Flat obj, int p, int r, int b) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return scalar(t, obj, p, b);
            case GoType.C_F64:
                return Double.longBitsToDouble(obj.prims[p]);
            case GoType.C_BOOL:
                return obj.bytes[b] != 0;
            case GoType.C_REF:
                return obj.refs[r];
            default:
                return obj.sub(t, p, t.nPrim(), r, t.nRef(), b, t.nByte());
        }
    }

    /** The raw scalar of an integer or boolean leaf of type {@code t}, from its region. */
    private static long scalar(GoType t, Flat obj, int p, int b) {
        switch (t.u().kind) {
            case GoType.INT8:
                return obj.bytes[b];
            case GoType.UINT8, GoType.BOOL:
                return obj.bytes[b] & 0xFFL;
            default:
                return obj.prims[p];
        }
    }

    /** Stores a boxed carrier value of type {@code t} at a location (flat values are copied in). */
    public static void store(GoType t, Flat obj, int p, int r, int b, Object v) {
        switch (t.carrier()) {
            case GoType.C_I64:
                if (t.isByteLeaf()) {
                    obj.bytes[b] = (byte) (long) v;
                } else {
                    obj.prims[p] = (long) v;
                }
                break;
            case GoType.C_F64:
                obj.prims[p] = Double.doubleToRawLongBits((double) v);
                break;
            case GoType.C_BOOL:
                obj.bytes[b] = (byte) ((boolean) v ? 1 : 0);
                break;
            case GoType.C_REF:
                obj.refs[r] = v;
                break;
            default:
                obj.store(p, r, b, (Flat) v);
        }
    }

    // ---------------------------------------------------------------- equality

    /** Go {@code ==} on two values of static type {@code t} in carrier form. */
    @TruffleBoundary
    public static boolean equal(GoType t, Object a, Object b) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.STRING:
                return Str.eq((Str) a, (Str) b);
            case GoType.INTERFACE:
                return ifaceEqual(a, b);
            case GoType.STRUCT, GoType.ARRAY:
                return flatEqual(u, (Flat) a, 0, 0, 0, (Flat) b, 0, 0, 0);
            case GoType.POINTER, GoType.UNSAFE_POINTER:
                return Ptr.same(a, b);
            case GoType.SLICE, GoType.MAP, GoType.FUNC:
                throw new Uncomparable(t, false);
            case GoType.CHAN:
                return a == b;
            default:
                // Long, Double (NaN != NaN, +0 == -0), Boolean, Complex.
                if (a instanceof Double x) {
                    return x.doubleValue() == ((Double) b).doubleValue();
                }
                return a.equals(b);
        }
    }

    /** Go {@code ==} on two interface values (null: nil interface). */
    @TruffleBoundary
    public static boolean ifaceEqual(Object a, Object b) {
        if (a == null || b == null) {
            return a == b;
        }
        Iface x = (Iface) a;
        Iface y = (Iface) b;
        if (x.type != y.type) {
            return false;
        }
        try {
            return equal(x.type, x.data, y.data);
        } catch (Uncomparable u) {
            throw atInterface(u, x.type);
        }
    }

    /**
     * gc's efaceeq/typehash report the dynamic type of the interface value when that type is
     * not comparable as a whole; a comparable type (a struct with an interface field) passes
     * on the report of the interface value inside it.
     */
    private static Uncomparable atInterface(Uncomparable u, GoType dynamic) {
        return comparable(dynamic) ? u : new Uncomparable(dynamic, u.hashing);
    }

    /** Whether values of type {@code t} support {@code ==} (Go spec, "Comparison operators"). */
    public static boolean comparable(GoType t) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.SLICE, GoType.MAP, GoType.FUNC:
                return false;
            case GoType.ARRAY:
                return comparable(u.elem);
            case GoType.STRUCT:
                for (Field f : u.fields) {
                    if (!comparable(f.type)) {
                        return false;
                    }
                }
                return true;
            default:
                return true;
        }
    }

    /** Slot-wise {@code ==} of two values of structural type {@code u} at the given locations. */
    @TruffleBoundary
    public static boolean flatEqual(GoType u, Flat a, int pa, int ra, int ba, Flat b, int pb, int rb, int bb) {
        if (u.kind == GoType.ARRAY) {
            GoType e = u.elem;
            int np = e.nPrim();
            int nr = e.nRef();
            int nb = e.nByte();
            for (int i = 0; i < u.len; i++) {
                if (!slotEqual(e, a, pa + i * np, ra + i * nr, ba + i * nb, b, pb + i * np, rb + i * nr,
                        bb + i * nb)) {
                    return false;
                }
            }
            return true;
        }
        u.nPrim();
        for (Field f : u.fields) {
            if (!f.name.equals("_") && !slotEqual(f.type, a, pa + f.primOff, ra + f.refOff, ba + f.byteOff, b,
                    pb + f.primOff, rb + f.refOff, bb + f.byteOff)) {
                return false;
            }
        }
        return true;
    }

    private static boolean slotEqual(GoType t, Flat a, int pa, int ra, int ba, Flat b, int pb, int rb, int bb) {
        GoType u = t.u();
        switch (u.carrier()) {
            case GoType.C_I64, GoType.C_BOOL:
                return u.isByteLeaf() ? a.bytes[ba] == b.bytes[bb] : a.prims[pa] == b.prims[pb];
            case GoType.C_F64:
                return Double.longBitsToDouble(a.prims[pa]) == Double.longBitsToDouble(b.prims[pb]);
            case GoType.C_REF:
                return equal(u, a.refs[ra], b.refs[rb]);
            default:
                return flatEqual(u, a, pa, ra, ba, b, pb, rb, bb);
        }
    }

    // ---------------------------------------------------------------- hashing

    /** A hash consistent with {@link #equal} for a value of static type {@code t}. */
    @TruffleBoundary
    public static int hash(GoType t, Object v) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.INTERFACE:
                if (v == null) {
                    return 0;
                }
                Iface i = (Iface) v;
                try {
                    return i.type.key.hashCode() * 31 + hash(i.type, i.data);
                } catch (Uncomparable x) {
                    throw atInterface(x, i.type);
                }
            case GoType.STRUCT, GoType.ARRAY:
                return flatHash(u, (Flat) v, 0, 0, 0);
            case GoType.SLICE, GoType.MAP, GoType.FUNC:
                throw new Uncomparable(t, true);
            case GoType.POINTER, GoType.UNSAFE_POINTER, GoType.CHAN:
                return Unsafe.hashPointer(v);
            default:
                if (v instanceof Double d) {
                    return d == 0 ? 0 : d.hashCode();
                }
                return v.hashCode();
        }
    }

    private static int flatHash(GoType u, Flat a, int p, int r, int b) {
        int h = 17;
        if (u.kind == GoType.ARRAY) {
            GoType e = u.elem;
            for (int i = 0; i < u.len; i++) {
                h = h * 31 + slotHash(e, a, p + i * e.nPrim(), r + i * e.nRef(), b + i * e.nByte());
            }
            return h;
        }
        u.nPrim();
        for (Field f : u.fields) {
            if (!f.name.equals("_")) {
                h = h * 31 + slotHash(f.type, a, p + f.primOff, r + f.refOff, b + f.byteOff);
            }
        }
        return h;
    }

    private static int slotHash(GoType t, Flat a, int p, int r, int b) {
        GoType u = t.u();
        switch (u.carrier()) {
            case GoType.C_I64, GoType.C_BOOL:
                return u.isByteLeaf() ? a.bytes[b] : Long.hashCode(a.prims[p]);
            case GoType.C_F64:
                return hash(u, Double.longBitsToDouble(a.prims[p]));
            case GoType.C_REF:
                return hash(u, a.refs[r]);
            default:
                return flatHash(u, a, p, r, b);
        }
    }

    // ---------------------------------------------------------------- print

    /** The bytes {@code print}/{@code println} write for the arguments (gc runtime/print.go; floats in the shortest form that round-trips, as go1.27 prints them). */
    @TruffleBoundary
    public static byte[] format(GoType[] types, Object[] values, boolean newline) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        for (int i = 0; i < values.length; i++) {
            if (newline && i > 0) {
                out.write(' ');
            }
            formatOne(out, types[i], values[i]);
        }
        if (newline) {
            out.write('\n');
        }
        return out.toByteArray();
    }

    private static void ascii(ByteArrayOutputStream out, String s) {
        byte[] b = s.getBytes(StandardCharsets.UTF_8);
        out.write(b, 0, b.length);
    }

    private static void formatOne(ByteArrayOutputStream out, GoType t, Object v) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.BOOL -> ascii(out, (boolean) v ? "true" : "false");
            case GoType.STRING -> out.write(((Str) v).b, ((Str) v).off, ((Str) v).len);
            case GoType.FLOAT32 -> ascii(out, GoFloatFmt.formatFloat((float) (double) v, 'g', -1, 32));
            case GoType.FLOAT64 -> ascii(out, GoFloatFmt.formatFloat((double) v, 'g', -1, 64));
            case GoType.COMPLEX64, GoType.COMPLEX128 -> {
                Complex c = (Complex) v;
                ascii(out, GoFloatFmt.formatComplex(c.re, c.im, 'g', -1, u.kind == GoType.COMPLEX64 ? 64 : 128));
            }
            case GoType.SLICE -> {
                Slice s = (Slice) v;
                ascii(out, "[" + s.len + "/" + s.cap + "]" + address(s.base));
            }
            case GoType.INTERFACE -> {
                Iface i = (Iface) v;
                ascii(out, i == null ? "(0x0,0x0)" : "(" + address(i.type) + "," + address(i) + ")");
            }
            case GoType.POINTER, GoType.UNSAFE_POINTER, GoType.MAP, GoType.CHAN, GoType.FUNC -> ascii(out, address(v));
            default -> {
                if (u.isInteger()) {
                    ascii(out, u.isUnsigned() ? Long.toUnsignedString((long) v) : Long.toString((long) v));
                } else {
                    throw new IllegalStateException("print of " + t.key);
                }
            }
        }
    }

    /** A stand-in for a machine address: nil is 0x0, anything else a stable fake. */
    private static String address(Object o) {
        return o == null ? "0x0" : "0xc" + String.format("%09x", System.identityHashCode(o));
    }
}
