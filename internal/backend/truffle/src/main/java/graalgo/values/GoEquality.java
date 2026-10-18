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
 *
 * <p>Hash/equal contract: IEEE {@code ==} equates {@code +0.0} and
 * {@code -0.0}, so {@link #hash} normalizes signed zeros before hashing
 * (scalars, float array elements, and complex components alike); equal
 * keys always hash equal.
 */
public final class GoEquality {
    private GoEquality() {}

    public static boolean equal(Object a, Object b) {
        if (a == b) {
            // Reference identity — and the (null, null) case, for which
            // canHideNaN(null) is false. Identity implies Go equality
            // except where NaN can hide: a NaN scalar never equals
            // anything, including itself, and a composite carrier can hide
            // NaN in a struct field, array element, complex component, or
            // interface payload, making Go's == false even against itself
            // (differential probe vs gc, review 2026-10-18). Those fall
            // through to the IEEE field-wise comparison below.
            if (canHideNaN(a)) {
                if (a instanceof Double d) {
                    return !d.isNaN();
                }
                if (a instanceof Float f) {
                    return !f.isNaN();
                }
                if (a instanceof GoComplex32 c) {
                    return !Float.isNaN(c.re()) && !Float.isNaN(c.im());
                }
                if (a instanceof GoComplex64 c) {
                    return !Double.isNaN(c.re()) && !Double.isNaN(c.im());
                }
                // Composite carrier: fall through to the structural
                // comparison, which applies IEEE semantics throughout.
            } else {
                return true;
            }
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
        if (a instanceof GoComplex32 ca && b instanceof GoComplex32 cb) {
            // Scalar complex64 (e.g. a struct field; review, 2026-10-18):
            // IEEE per-component semantics — +0.0 == -0.0, and a NaN
            // component is never equal (x == y is already false for NaN).
            return ca.re() == cb.re() && ca.im() == cb.im();
        }
        if (a instanceof GoComplex64 ca && b instanceof GoComplex64 cb) {
            // Scalar complex128; same IEEE per-component semantics.
            return ca.re() == cb.re() && ca.im() == cb.im();
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
     * Whether reference identity does <em>not</em> imply Go {@code ==}
     * equality: float/complex scalars (a NaN never equals anything,
     * including itself) and composite carriers that can hide NaN in a
     * struct field, array element, complex component, or interface
     * payload (Go's {@code ==} is false for those even against
     * themselves). Used by {@link #equal}'s identity shortcut.
     */
    private static boolean canHideNaN(Object a) {
        return a instanceof Double || a instanceof Float
                || a instanceof GoComplex32 || a instanceof GoComplex64
                || a instanceof GoStruct || a instanceof GoInterface
                || a instanceof double[] || a instanceof float[]
                || a instanceof GoComplex32[] || a instanceof GoComplex64[]
                || a instanceof Object[];
    }

    /**
     * Hash of a float64 map-key component, consistent with {@link #equal}:
     * IEEE {@code ==} equates {@code +0.0} and {@code -0.0}, so signed
     * zeros are normalized to {@code +0.0} before hashing. NaN needs no
     * normalization — it never equals anything (including itself), so its
     * canonical hash is a mere collision, never a contract break.
     *
     * <p>Package-private (not private): {@link GoStruct#hashCode} funnels
     * float struct fields through the same normalizer.
     */
    static int hashDouble(double v) {
        if (v == 0) {
            v = 0.0; // normalize -0.0
        }
        return Double.hashCode(v);
    }

    /**
     * As {@link #hashDouble}, for float32 map-key components.
     * Package-private for {@link GoStruct#hashCode}, as above.
     */
    static int hashFloat(float v) {
        if (v == 0) {
            v = 0.0f; // normalize -0.0f
        }
        return Float.hashCode(v);
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
            return hashDouble(d);
        }
        if (key instanceof Float f) {
            return hashFloat(f);
        }
        if (key instanceof GoComplex32 c) {
            // Scalar complex64 (e.g. a struct field; review, 2026-10-18):
            // consistent with equal above; signed-zero components
            // normalized via hashFloat, mirroring the GoComplex32[] branch.
            int h = 1;
            h = 31 * h + hashFloat(c.re());
            h = 31 * h + hashFloat(c.im());
            return h;
        }
        if (key instanceof GoComplex64 c) {
            // Scalar complex128; signed-zero components normalized via
            // hashDouble, mirroring the GoComplex64[] branch.
            int h = 1;
            h = 31 * h + hashDouble(c.re());
            h = 31 * h + hashDouble(c.im());
            return h;
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
        if (key instanceof byte[] ba) {
            return java.util.Arrays.hashCode(ba);
        }
        if (key instanceof short[] sa) {
            return java.util.Arrays.hashCode(sa);
        }
        if (key instanceof int[] ia) {
            return java.util.Arrays.hashCode(ia);
        }
        if (key instanceof double[] da) {
            // P3-M9 (arrdispatch): [N]float64 carrier hash; element hashes
            // go through hashDouble so that equal (+0.0/-0.0) elements
            // hash equal. This is exactly Arrays.hashCode(double[])'s
            // 31*h+c accumulation, with signed-zero normalization.
            int h = 1;
            for (double v : da) {
                h = 31 * h + hashDouble(v);
            }
            return h;
        }
        if (key instanceof float[] fa) {
            // P3-M9 (arrdispatch): [N]float32 carrier hash; as above.
            int h = 1;
            for (float v : fa) {
                h = 31 * h + hashFloat(v);
            }
            return h;
        }
        if (key instanceof GoComplex32[] ca) {
            // P3-M9 (arrdispatch): [N]complex64 carrier hash; consistent
            // with equal above (null elements hash as the zero value;
            // signed-zero components normalized via hashFloat).
            int h = 1;
            for (GoComplex32 c : ca) {
                GoComplex32 z = c == null ? GoComplex32.ZERO : c;
                h = 31 * h + hashFloat(z.re());
                h = 31 * h + hashFloat(z.im());
            }
            return h;
        }
        if (key instanceof GoComplex64[] ca) {
            // P3-M9 (arrdispatch): [N]complex128 carrier hash; signed-zero
            // components normalized via hashDouble.
            int h = 1;
            for (GoComplex64 c : ca) {
                GoComplex64 z = c == null ? GoComplex64.ZERO : c;
                h = 31 * h + hashDouble(z.re());
                h = 31 * h + hashDouble(z.im());
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
