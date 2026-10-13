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

    /** Hash consistent with {@link #equal}: equal keys hash equal. */
    public static int hash(Object key) {
        if (key instanceof Long l) {
            return Long.hashCode(l);
        }
        if (key instanceof Integer i) {
            return Integer.hashCode(i);
        }
        if (key instanceof Double d) {
            return Double.hashCode(d);
        }
        if (key instanceof Float f) {
            return Float.hashCode(f);
        }
        if (key instanceof Boolean b) {
            return Boolean.hashCode(b);
        }
        if (key instanceof GoString s) {
            return s.hashCode();
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
        if (key instanceof int[] ia) {
            return java.util.Arrays.hashCode(ia);
        }
        if (key instanceof double[] da) {
            return java.util.Arrays.hashCode(da);
        }
        if (key instanceof float[] fa) {
            // P3-M9 (arrdispatch): [N]float32 carrier hash.
            return java.util.Arrays.hashCode(fa);
        }
        if (key instanceof GoComplex32[] ca) {
            // P3-M9 (arrdispatch): [N]complex64 carrier hash; consistent
            // with equal above (null elements hash as the zero value).
            int h = 1;
            for (GoComplex32 c : ca) {
                GoComplex32 z = c == null ? GoComplex32.ZERO : c;
                h = 31 * h + Float.hashCode(z.re());
                h = 31 * h + Float.hashCode(z.im());
            }
            return h;
        }
        if (key instanceof GoComplex64[] ca) {
            // P3-M9 (arrdispatch): [N]complex128 carrier hash.
            int h = 1;
            for (GoComplex64 c : ca) {
                GoComplex64 z = c == null ? GoComplex64.ZERO : c;
                h = 31 * h + Double.hashCode(z.re());
                h = 31 * h + Double.hashCode(z.im());
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
}
