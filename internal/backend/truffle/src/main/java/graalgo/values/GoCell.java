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

    /**
     * Hash consistent with {@link #sameStorageAs}: cells addressing the
     * same storage hash equal, so pointer map keys and interface
     * comparisons agree. The default (object identity) suits cells
     * whose storage identity IS object identity; field and element
     * cells override with their (owner, index) identity.
     */
    default int storageHash() {
        return System.identityHashCode(this);
    }

    /**
     * Canonical key for storage-identity hashing (fix-redmaster-r43):
     * two cells addressing the same storage yield equal keys;
     * distinct storages yield unequal keys. The default
     * ({@code this}) suits cells whose object identity IS their
     * storage identity (GoBox); field and element cells override
     * (fresh objects per {@code &} evaluation, same storage).
     */
    default Object storageKey() {
        return this;
    }

    /**
     * Identity-based (owner, index) storage key for field and
     * element cells. Identity, not {@code equals}: a struct owner
     * overrides {@code equals} structurally, but two equal structs
     * are distinct storages and must key distinctly.
     */
    final class StorageKey {
        private final Object owner;
        private final int index;

        public StorageKey(Object owner, int index) {
            this.owner = owner;
            this.index = index;
        }

        @Override
        public boolean equals(Object o) {
            return o instanceof StorageKey k && k.owner == owner && k.index == index;
        }

        @Override
        public int hashCode() {
            return 31 * System.identityHashCode(owner) + index;
        }
    }

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
}
