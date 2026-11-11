package graalgo.values;

/**
 * A Go {@code complex64} value (WS G): a pair of float32 components.
 *
 * <p>Per PLAN.md D5's carrier rules, complex64's components are each a
 * Java {@code float} (correct float32 rounding is semantic, as for
 * {@code float32} itself). Complex values travel in {@code Object} frame
 * slots and in {@code GoComplex32[]}/{@code GoComplex64[]} typed arrays.
 */
public record GoComplex32(float re, float im) {
    /** Zero value. */
    public static final GoComplex32 ZERO = new GoComplex32(0f, 0f);

    /**
     * Go {@code ==} semantics (D-P1-1): the record's generated
     * {@code equals} would use {@link Float#equals}, under which NaN
     * equals NaN and -0.0f differs from 0.0f — both wrong for Go, where
     * a NaN component never equals anything (including itself) and
     * -0.0 == +0.0. Components compare with {@code ==}, exactly as
     * {@link GoEquality#equal} does for scalar complex64.
     */
    @Override
    public boolean equals(Object o) {
        return o instanceof GoComplex32 c && re == c.re() && im == c.im();
    }

    /**
     * Hash consistent with {@link #equals} (D-P1-1): -0.0f canonicalizes
     * to +0.0f before hashing, so equal values hash equal
     * ({@link Float#hashCode} alone distinguishes -0.0f/+0.0f). NaN
     * needs no special case: {@link Float#hashCode} canonicalizes NaN
     * payloads via {@link Float#floatToIntBits}.
     */
    @Override
    public int hashCode() {
        return 31 * hashComponent(re) + hashComponent(im);
    }

    private static int hashComponent(float v) {
        return Float.hashCode(v == 0.0f ? 0.0f : v);
    }

    /** Widening conversion to complex128 (exact: float32 → float64 widens). */
    public GoComplex64 widen() {
        return new GoComplex64(re, im);
    }
}
