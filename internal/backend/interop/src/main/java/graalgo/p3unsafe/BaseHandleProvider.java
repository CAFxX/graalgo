package graalgo.p3unsafe;

/**
 * Implemented by interned referent keys that denote <em>derived</em>
 * references — keys whose resolution goes through other handles (RV-P1-23).
 *
 * <p>The reflect worker's {@code Ref} holders (struct fields, array/slice
 * elements) resolve their base lazily: a child {@code reflect.Value}'s
 * handle denotes a key that only resolves via its base handle. gc keeps
 * the parent object alive through the child's pointer, so a child Value
 * must own the whole base chain. {@link UnsafeTable#countedClosure} walks
 * this mapping to compute the ownership set; the walk is cycle-safe.
 */
public interface BaseHandleProvider {
    /**
     * The handles this key's resolution depends on (its base chain links).
     * Implementations return a fresh array; entries are never 0.
     */
    long[] baseHandles();
}
