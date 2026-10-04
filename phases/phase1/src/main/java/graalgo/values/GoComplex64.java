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
}
