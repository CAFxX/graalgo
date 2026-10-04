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

    /** Widening conversion to complex128 (exact: float32 → float64 widens). */
    public GoComplex64 widen() {
        return new GoComplex64(re, im);
    }
}
