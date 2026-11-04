package graalgo.values;

/**
 * Go {@code ==} equality and matching hashes for map keys and interface
 * comparisons (D5/D6).
 *
 * <p>Rules: numeric kinds compare by value within their Java carrier
 * ({@code long} for all int/uint kinds, {@code double} for both float
 * kinds); IEEE semantics apply, so a NaN key never equals anything —
 * including itself — matching Go. {@link GoString} compares byte-wise.
 * Values of different carrier classes are never equal (a Go map has a
 * single static key type, so this only arises from misuse).
 */
public final class GoEquality {
    private GoEquality() {}

    public static boolean equal(Object a, Object b) {
        if (a == b) {
            // Covers reference-identity and the (null, null) case. Note: for
            // Doubles this would wrongly equate two identical NaN
            // references, so NaN is checked below even on this path.
            if (a instanceof Double d && d.isNaN()) {
                return false;
            }
            if (a instanceof Float f && f.isNaN()) {
                return false;
            }
            return true;
        }
        if (a == null || b == null) {
            return false;
        }
        if (a instanceof Long la && b instanceof Long lb) {
            return la.longValue() == lb.longValue();
        }
        if (a instanceof Integer ia && b instanceof Integer ib) {
            return ia.intValue() == ib.intValue();
        }
        if (a instanceof Double da && b instanceof Double db) {
            double x = da, y = db;
            return x == y && !Double.isNaN(x);
        }
        if (a instanceof Float fa && b instanceof Float fb) {
            float x = fa, y = fb;
            return x == y && !Float.isNaN(x);
        }
        if (a instanceof Boolean ba && b instanceof Boolean bb) {
            return ba.booleanValue() == bb.booleanValue();
        }
        if (a instanceof GoString sa && b instanceof GoString sb) {
            return sa.equals(sb);
        }
        if (a instanceof GoStruct sa && b instanceof GoStruct sb) {
            // Struct map keys (M4, WS E): comparable struct types only
            // reach here (go/types rejects uncomparable key types).
            return sa.equals(sb);
        }
        if (a instanceof GoPointer ga && b instanceof GoPointer gb) {
            // Go == on pointers compares addressed storage, exactly
            // like the BinaryOpNode POINTER branch; without this,
            // pointer-valued interface data (notably *rtype values
            // from toRType) compared unequal unless identical objects
            // (fix-redmaster-r43: TypeOf(0) == TypeOf(0) was false).
            return ga.sameStorageAs(gb);
        }
        if (a instanceof GoInterface ia && b instanceof GoInterface ib) {
            // P3-M9 (tier-1d): interface map keys (e.g. unique's any-held
            // struct keys). Nil == nil; else same dynamic type (descriptor
            // identity) and equal dynamic values.
            if (ia.isNil() || ib.isNil()) {
                return ia.isNil() && ib.isNil();
            }
            return ia.descriptor() == ib.descriptor() && equal(ia.data(), ib.data());
        }
        if (a instanceof long[] la && b instanceof long[] lb) {
            // P3-M9 (tier-1d): array fields in struct keys (e.g. unique's
            // struct{v [3]int} keys).
            return java.util.Arrays.equals(la, lb);
        }
        if (a instanceof byte[] ba2 && b instanceof byte[] bb2) {
            // Array map keys (e.g. [16]byte UUID keys).
            return java.util.Arrays.equals(ba2, bb2);
        }
        if (a instanceof short[] sa2 && b instanceof short[] sb2) {
            // Array map keys (e.g. [8]int16 keys).
            return java.util.Arrays.equals(sa2, sb2);
        }
        if (a instanceof int[] ia2 && b instanceof int[] ib2) {
            return java.util.Arrays.equals(ia2, ib2);
        }
        if (a instanceof double[] da && b instanceof double[] db) {
            // P3-M9 (arrdispatch): IEEE semantics — a NaN element never
            // equals anything, including itself (matches the scalar Double
            // branch above; Arrays.equals would wrongly equate NaNs, which
            // also contradicted this class's documented contract).
            if (da.length != db.length) {
                return false;
            }
            for (int i = 0; i < da.length; i++) {
                if (da[i] != db[i] || Double.isNaN(da[i])) {
                    return false;
                }
            }
            return true;
        }
        if (a instanceof float[] fa && b instanceof float[] fb) {
            // P3-M9 (arrdispatch): [N]float32 carrier; IEEE semantics as
            // for double[] above.
            if (fa.length != fb.length) {
                return false;
            }
            for (int i = 0; i < fa.length; i++) {
                if (fa[i] != fb[i] || Float.isNaN(fa[i])) {
                    return false;
                }
            }
            return true;
        }
        if (a instanceof GoComplex32 ca && b instanceof GoComplex32 cb) {
            // Scalar complex64: component-wise IEEE equality, mirroring
            // the array branch below (-0 == +0 per component; a NaN
            // component never equals anything, via Java == semantics).
            return ca.re() == cb.re() && ca.im() == cb.im();
        }
        if (a instanceof GoComplex64 ca && b instanceof GoComplex64 cb) {
            // Scalar complex128: same IEEE rule as above. Records box
            // fresh instances per Boxing.box, so reference equality above
            // cannot be relied on for scalar complex keys.
            return ca.re() == cb.re() && ca.im() == cb.im();
        }
        if (a instanceof GoComplex32[] ca && b instanceof GoComplex32[] cb) {
            // P3-M9 (arrdispatch): [N]complex64 carrier. Null elements are
            // the zero value (unfilled slots of a var-declared array).
            if (ca.length != cb.length) {
                return false;
            }
            for (int i = 0; i < ca.length; i++) {
                GoComplex32 x = ca[i] == null ? GoComplex32.ZERO : ca[i];
                GoComplex32 y = cb[i] == null ? GoComplex32.ZERO : cb[i];
                if (x.re() != y.re() || x.im() != y.im()) {
                    return false;
                }
            }
            return true;
        }
        if (a instanceof GoComplex64[] ca && b instanceof GoComplex64[] cb) {
            // P3-M9 (arrdispatch): [N]complex128 carrier; null-safe as above.
            if (ca.length != cb.length) {
                return false;
            }
            for (int i = 0; i < ca.length; i++) {
                GoComplex64 x = ca[i] == null ? GoComplex64.ZERO : ca[i];
                GoComplex64 y = cb[i] == null ? GoComplex64.ZERO : cb[i];
                if (x.re() != y.re() || x.im() != y.im()) {
                    return false;
                }
            }
            return true;
        }
        if (a instanceof boolean[] ba && b instanceof boolean[] bb) {
            return java.util.Arrays.equals(ba, bb);
        }
        if (a instanceof Object[] oa && b instanceof Object[] ob) {
            if (oa.length != ob.length) {
                return false;
            }
            for (int i = 0; i < oa.length; i++) {
                if (!equal(oa[i], ob[i])) {
                    return false;
                }
            }
            return true;
        }
        return false;
    }

    /**
     * Hash of a float64 with -0.0 normalized to +0.0, so IEEE-equal
     * values (equal() treats -0.0 == +0.0) hash equal. NaN passes
     * through: Double.hashCode canonicalizes NaN bits, and NaN keys
     * never equal anything, so their hash value cannot violate the
     * equal => same-hash contract.
     */
    private static int doubleHash(double d) {
        return Double.hashCode(d == 0d ? 0d : d);
    }

    /** Float32 analogue of {@link #doubleHash(double)}. */
    private static int floatHash(float f) {
        return Float.hashCode(f == 0f ? 0f : f);
    }

    /** Hash consistent with {@link #equal}: equal keys hash equal. */
    public static int hash(Object key) {
        if (key instanceof Long l) {
            return Long.hashCode(l);
        }
        if (key instanceof Integer i) {
            return Integer.hashCode(i);
        }
        if (key instanceof Double d) {
            return doubleHash(d);
        }
        if (key instanceof Float f) {
            return floatHash(f);
        }
        if (key instanceof Boolean b) {
            return Boolean.hashCode(b);
        }
        if (key instanceof GoString s) {
            return s.hashCode();
        }
        if (key instanceof GoPointer gp) {
            return gp.storageHash();
        }
        if (key instanceof GoStruct gs) {
            return gs.hashCode();
        }
        if (key instanceof GoInterface gi) {
            // P3-M9 (tier-1d): consistent with the interface equality above.
            if (gi.isNil()) {
                return 0;
            }
            int h = System.identityHashCode(gi.descriptor());
            h = 31 * h + hash(gi.data());
            return h;
        }
        if (key instanceof long[] la) {
            return java.util.Arrays.hashCode(la);
        }
        if (key instanceof byte[] ba) {
            return java.util.Arrays.hashCode(ba);
        }
        if (key instanceof short[] sa) {
            return java.util.Arrays.hashCode(sa);
        }
        if (key instanceof int[] ia) {
            return java.util.Arrays.hashCode(ia);
        }
        if (key instanceof GoComplex32 c) {
            // Scalar complex64: same 31-mix shape as the array branch,
            // with -0 components normalized (see doubleHash).
            return 31 * floatHash(c.re()) + floatHash(c.im());
        }
        if (key instanceof GoComplex64 c) {
            // Scalar complex128: same shape as the complex64 branch.
            return 31 * doubleHash(c.re()) + doubleHash(c.im());
        }
        if (key instanceof double[] da) {
            // [N]float64 carrier hash with -0 elements normalized;
            // Arrays.hashCode would hash -0.0 differently from +0.0.
            int h = 1;
            for (double v : da) {
                h = 31 * h + doubleHash(v);
            }
            return h;
        }
        if (key instanceof float[] fa) {
            // P3-M9 (arrdispatch): [N]float32 carrier hash, -0 normalized.
            int h = 1;
            for (float v : fa) {
                h = 31 * h + floatHash(v);
            }
            return h;
        }
        if (key instanceof GoComplex32[] ca) {
            // P3-M9 (arrdispatch): [N]complex64 carrier hash; consistent
            // with equal above (null elements hash as the zero value),
            // with -0 components normalized.
            int h = 1;
            for (GoComplex32 c : ca) {
                GoComplex32 z = c == null ? GoComplex32.ZERO : c;
                h = 31 * h + floatHash(z.re());
                h = 31 * h + floatHash(z.im());
            }
            return h;
        }
        if (key instanceof GoComplex64[] ca) {
            // P3-M9 (arrdispatch): [N]complex128 carrier hash, -0 normalized.
            int h = 1;
            for (GoComplex64 c : ca) {
                GoComplex64 z = c == null ? GoComplex64.ZERO : c;
                h = 31 * h + doubleHash(z.re());
                h = 31 * h + doubleHash(z.im());
            }
            return h;
        }
        if (key instanceof boolean[] ba) {
            return java.util.Arrays.hashCode(ba);
        }
        if (key instanceof Object[] oa) {
            int h = 1;
            for (Object o : oa) {
                h = 31 * h + hash(o);
            }
            return h;
        }
        // Opaque reference keys (pointers, interfaces): identity hash.
        return System.identityHashCode(key);
    }

    /**
     * Returns the Go-syntax type name when {@code key} is of an
     * unhashable (uncomparable) Go type — slices, maps, funcs, and
     * structs/arrays containing one — or null when the key is hashable.
     * Callers (GoMap, MapKeys) panic with gc's
     * "hash of unhashable type &lt;T&gt;" for a non-null result instead
     * of silently inserting phantom entries.
     *
     * <p>Name accuracy: for interface-wrapped keys the dynamic-type
     * descriptor's name is exact (gc's name, e.g. "[]int", "main.S",
     * "func()") — guest keys always pass through MapKeys.box, which
     * preserves the descriptor. Bare carriers carry no type info, so
     * their names are best-effort from the carrier (a long[]-backed
     * slice reports "[]int64" even when the Go type was "[]int";
     * an empty reference-element slice reports "[]any").
     */
    public static String unhashableTypeName(Object key) {
        if (key == null) {
            return null;
        }
        if (key instanceof GoInterface gi) {
            if (gi.isNil()) {
                return null;
            }
            // The frontend marks slice/map/func (and structs/arrays
            // containing one) non-comparable on the descriptor, so this
            // fires even for a typed-nil value (data == null), matching
            // gc: the TYPE is unhashable regardless of the value.
            if (gi.descriptor() instanceof GoTypeDescriptor td && !td.isComparable()) {
                return td.name();
            }
            String inner = unhashableTypeName(gi.data());
            if (inner != null) {
                return gi.descriptor() instanceof GoTypeDescriptor td ? td.name() : inner;
            }
            return null;
        }
        if (key instanceof GoSlice gs) {
            return "[]" + sliceElemName(gs.array());
        }
        if (key instanceof GoMap) {
            // Bare map carrier: key/value types did not survive boxing.
            return "map[any]any";
        }
        if (key instanceof GoFunc) {
            return "func()";
        }
        if (key instanceof GoStruct gs) {
            // Kind-based first: covers typed fields even when the value
            // slot is null (reflect shells). Value-based second: covers
            // fields whose descriptor metadata is absent.
            String byKind = unhashableStructDescriptorName(gs.descriptor());
            if (byKind != null) {
                return byKind;
            }
            for (int i = 0; i < gs.descriptor().fields().size(); i++) {
                GoStructDescriptor.Field f = gs.descriptor().field(i);
                if (f.inLongs()) {
                    continue;
                }
                String n = unhashableTypeName(gs.getObject(i));
                if (n != null) {
                    return gs.descriptor().typeName();
                }
            }
            return null;
        }
        if (key instanceof Object[] oa) {
            // Array of an unhashable element type (e.g. [N][]int).
            for (Object e : oa) {
                String n = unhashableTypeName(e);
                if (n != null) {
                    return n;
                }
            }
            return null;
        }
        // Scalars, strings, pointers, chans, and opaque references are hashable.
        return null;
    }

    /** Descriptor-only unhashability for a struct type, or null if hashable. */
    private static String unhashableStructDescriptorName(GoStructDescriptor d) {
        for (GoStructDescriptor.Field f : d.fields()) {
            switch (f.kind()) {
                case SLICE, MAP, FUNC -> {
                    return d.typeName();
                }
                case STRUCT -> {
                    if (f.structDesc() != null
                            && unhashableStructDescriptorName(f.structDesc()) != null) {
                        return d.typeName();
                    }
                }
                case ARRAY -> {
                    if (f.arrayElemKind() == GoKind.SLICE || f.arrayElemKind() == GoKind.MAP
                            || f.arrayElemKind() == GoKind.FUNC) {
                        return d.typeName();
                    }
                    if (f.arrayElemKind() == GoKind.STRUCT && f.arrayElemDesc() != null
                            && unhashableStructDescriptorName(f.arrayElemDesc()) != null) {
                        return d.typeName();
                    }
                }
                default -> {
                }
            }
        }
        return null;
    }

    /** Best-effort Go element-type name for a slice backing array. */
    private static String sliceElemName(Object array) {
        Class<?> ct = array.getClass().getComponentType();
        if (ct == null) {
            return "any";
        }
        if (ct.isPrimitive()) {
            if (ct == byte.class) {
                return "uint8";
            }
            if (ct == short.class) {
                return "int16";
            }
            if (ct == int.class) {
                return "int32";
            }
            if (ct == long.class) {
                return "int64";
            }
            if (ct == float.class) {
                return "float32";
            }
            if (ct == double.class) {
                return "float64";
            }
            if (ct == boolean.class) {
                return "bool";
            }
            return "any";
        }
        if (ct == GoString.class) {
            return "string";
        }
        if (ct == GoComplex32.class) {
            return "complex64";
        }
        if (ct == GoComplex64.class) {
            return "complex128";
        }
        // Reference-element backing (Object[], GoStruct[]): name from the
        // first informative element, if any.
        int n = java.lang.reflect.Array.getLength(array);
        for (int i = 0; i < n; i++) {
            Object e = java.lang.reflect.Array.get(array, i);
            if (e instanceof GoStruct gs) {
                return gs.descriptor().typeName();
            }
            if (e instanceof GoString) {
                return "string";
            }
            if (e instanceof GoComplex32) {
                return "complex64";
            }
            if (e instanceof GoComplex64) {
                return "complex128";
            }
            if (e instanceof GoInterface gi
                    && gi.descriptor() instanceof GoTypeDescriptor td) {
                return td.name();
            }
        }
        return "any";
    }
}
