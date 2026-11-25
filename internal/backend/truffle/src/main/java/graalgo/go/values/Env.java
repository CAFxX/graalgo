package graalgo.go.values;

/**
 * The capture environment of a closure — the one place that knows its representation (SPEC
 * §3.4, ADR-0023, truffle-design amendment A9). The layout is fixed by the capture count
 * {@code size}, which every creator and reader knows statically (the literal's captures, a
 * bound receiver, an intrinsic's own state):
 *
 * <ul>
 *   <li>{@code size == 0}: no environment ({@code null}); the closure is a constant.
 *   <li>{@code size == 1}: the captured value itself (a {@link Flat} box, a bound receiver);
 *       no allocation beyond the {@link Closure}.
 *   <li>{@code size >= 2}: an {@code Object[size]}, one element per captured value.
 * </ul>
 *
 * <p>A captured variable is captured by reference through its box, so the environment itself is
 * never written after creation: escape analysis scalar-replaces it with the closure when the
 * func value does not escape.
 */
public final class Env {
    private Env() {}

    /** An environment of {@code size >= 2} values, filled with {@link #set}. */
    public static Object[] create(int size) {
        assert size >= 2;
        return new Object[size];
    }

    public static void set(Object[] env, int index, Object value) {
        env[index] = value;
    }

    /** Value {@code index} of an environment of {@code size} values. */
    public static Object get(Object env, int index, int size) {
        assert size >= 1 && index < size;
        return size == 1 ? env : ((Object[]) env)[index];
    }
}
