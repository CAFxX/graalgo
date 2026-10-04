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
        // Opaque reference keys (pointers, interfaces): identity hash.
        return System.identityHashCode(key);
    }
}
