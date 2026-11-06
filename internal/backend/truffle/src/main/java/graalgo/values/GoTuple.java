package graalgo.values;

/**
 * The value of a multi-value Go expression: the ordered results of a call
 * to a function returning two or more values (V12-P1-2).
 *
 * <p>A multi-value call has no single Go value: its results travel together
 * as one carrier object from the callee's root node to the caller's
 * destructuring site ({@code return}, {@code assign}, {@code vardecl} in
 * {@code NodeFactoryV2}), which splits the carrier into the individual
 * result slots. The carrier never escapes into a single-value position:
 * the checker rejects programs that use a multi-value call where one
 * value is expected, and the factory fails loudly if a tuple-typed
 * expression reaches any other consumer.
 *
 * <p>Values are stored in canonical boxed form (see {@code Boxing.box}):
 * primitives as {@link Long}/{@link Integer}/{@link Double} etc., strings
 * as {@link GoString}. The array is created fresh per evaluation and never
 * mutated after construction, so no copy is taken here.
 */
public final class GoTuple {
    private final Object[] values;

    public GoTuple(Object[] values) {
        if (values == null) {
            throw new IllegalArgumentException("GoTuple values must not be null");
        }
        this.values = values;
    }

    /** Number of results in the tuple (0 only for the empty tuple, unused as a value). */
    public int size() {
        return values.length;
    }

    /** The i-th result, in canonical boxed form. */
    public Object get(int i) {
        return values[i];
    }

    @Override
    public String toString() {
        return "GoTuple(" + values.length + " values)";
    }
}
