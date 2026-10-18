package graalgo.runtime.sync;

/**
 * A Go value cell that produces a fresh, independent copy of itself when a
 * Go value is copied by assignment.
 *
 * <p>Go assignment copies struct values; sync and atomic wrapper cells are
 * opaque Java objects that must never be shared across copies — sharing
 * would let an operation through one copy be observed through another,
 * which gc's value copy does not do. Every copy site (struct field copies,
 * array element copies, by-value parameter passing, zero-value templates)
 * dispatches on this interface, never on concrete class names: a new cell
 * type only has to implement {@code Copyable} to be copied correctly
 * everywhere (RV18-P1-16).
 *
 * <p>Fresh-copy semantics per implementor: sync primitives ({@code Mutex},
 * {@code RWMutex}, {@code WaitGroup}, {@code Once}, {@code Pool}) copy as
 * fresh, pristine instances; atomic cells copy the current integer value.
 */
public interface Copyable {
    /**
     * Returns a fresh, independent copy of this cell: same Go value,
     * no shared mutable state with the original.
     */
    Object freshCopy();
}
