package graalgo.values;

/**
 * A mutable cell holding one Go value in canonical boxed form.
 *
 * <p>A Go pointer ({@link GoPointer}) does not point at a value snapshot:
 * it points at the variable's (or field's, or element's) storage, so that
 * {@code *p = v} is observable through every alias of that storage and
 * vice versa. {@code GoCell} is the common interface over the storage
 * shapes a pointer can address:
 * <ul>
 *   <li>{@link GoBox} — a local variable, parameter, or global whose
 *       address was taken (P3-M9, WS D);</li>
 *   <li>{@link GoFieldCell} — a struct field ({@code &s.f});</li>
 *   <li>{@link GoArrayCell} — a slice/array element ({@code &s[i]}).</li>
 * </ul>
 *
 * <p>{@code get} returns the canonical boxed form (the same form a
 * {@code GoBox} cell holds); {@code set} stores a canonical boxed value,
 * applying the carrier's encoding (e.g. the {@code long[]} struct carrier
 * for {@code inLongs} fields, the typed Java array for elements).
 */
public interface GoCell {
    /** The current value in canonical boxed form. */
    Object get();

    /** Stores a canonical boxed value. */
    void set(Object value);
}
