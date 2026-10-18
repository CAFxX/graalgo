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

    /**
     * Storage identity: true iff this cell and {@code other} address the
     * same Go storage — the same variable, the same field of the same
     * struct value, or the same element of the same backing array.
     *
     * <p>Two evaluations of {@code &s.f} (or {@code &s[i]}) produce
     * distinct cell objects ({@link graalgo.frontend.GoFieldCell} is
     * allocated fresh per evaluation), so Java object identity is the
     * wrong equivalence: gc compares the addressed <em>storage</em>, and
     * {@code p == q} on pointers must too (via
     * {@link GoPointer#sameStorageAs}).
     */
    boolean sameStorageAs(GoCell other);

    /**
     * Storage-identity hash, consistent with {@link #sameStorageAs}: whenever
     * {@code a.sameStorageAs(b)} is true, {@code a.storageHash() ==
     * b.storageHash()} must hold. Needed because storage identity (not Java
     * object identity) is the equivalence the finalizer registry keys on —
     * two distinct cell objects over the same storage must hash alike.
     *
     * <p>Polymorphic by design (like {@code sameStorageAs}): each cell shape
     * hashes exactly the components its {@code sameStorageAs} compares, so no
     * caller needs {@code instanceof} dispatch over cell types.
     */
    int storageHash();
}
