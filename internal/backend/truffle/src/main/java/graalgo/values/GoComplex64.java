package graalgo.values;

/**
 * A Go {@code complex128} value (WS G): a pair of float64 components.
 *
 * <p>Complex values travel in {@code Object} frame slots and in typed
 * arrays; see {@link GoComplex32} for the complex64 carrier.
 */
public record GoComplex64(double re, double im) {
    /** Zero value. */
    public static final GoComplex64 ZERO = new GoComplex64(0d, 0d);

    /**
     * Go {@code ==} semantics (D-P1-1): the record's generated
     * {@code equals} would use {@link Double#equals}, under which NaN
     * equals NaN and -0.0 differs from 0.0 — both wrong for Go, where a
     * NaN component never equals anything (including itself) and
     * -0.0 == +0.0. Components compare with {@code ==}, exactly as
     * {@link GoEquality#equal} does for scalar complex128.
     */
    @Override
    public boolean equals(Object o) {
        return o instanceof GoComplex64 c && re == c.re() && im == c.im();
    }

    /**
     * Hash consistent with {@link #equals} (D-P1-1): -0.0 canonicalizes
     * to +0.0 before hashing, so equal values hash equal
     * ({@link Double#hashCode} alone distinguishes -0.0/+0.0). NaN
     * needs no special case: {@link Double#hashCode} canonicalizes NaN
     * payloads via {@link Double#doubleToLongBits}.
     */
    @Override
    public int hashCode() {
        return 31 * hashComponent(re) + hashComponent(im);
    }

    private static int hashComponent(double v) {
        return Double.hashCode(v == 0.0 ? 0.0 : v);
    }
}
