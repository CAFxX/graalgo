package graalgo.values;

/**
 * A mutable cell holding one Go value in canonical boxed form
 * (per-iteration capture).
 *
 * <p>Go gives each loop iteration its own fresh copy of the variables the
 * loop declares (go1.22+; body-declared variables in all versions). A
 * closure created in iteration <i>k</i> must observe iteration <i>k</i>'s
 * variable even after the loop has moved on, so captured loop variables
 * cannot live in a plain frame slot that the next iteration reuses.
 *
 * <p>A boxed variable's frame slot holds a {@code GoBox} instead of the
 * raw value:
 * <ul>
 *   <li>body-declared and range variables: the declaration/assignment
 *       allocates a fresh {@code GoBox} every time it executes, so each
 *       iteration (and each captured closure) sees its own cell;</li>
 *   <li>{@code for}-header variables ({@code for i := 0; ...}): one
 *       per-iteration cell plus a <em>carry</em> cell, following gc's
 *       {@code loopvar} desugar ({@code z := z'} … {@code z' = z}):
 *       the init statement seeds the carry; each iteration installs a
 *       fresh per-iteration cell copied from the carry ({@link
 *       graalgo.frontend.BoxRefreshNode}, before the condition); init-seed,
 *       post, cond, body, and every closure created in the loop operate on
 *       the per-iteration cell; at the end of the iteration (normal and
 *       {@code continue} paths) the per-iteration value is written back
 *       into the carry ({@link graalgo.frontend.BoxWritebackNode}). A
 *       closure that captured iteration <i>j</i>'s cell writes only that
 *       (by then detached) cell — it can never corrupt the carry that
 *       seeds future iterations.</li>
 * </ul>
 *
 * <p>{@code value} is the canonical boxed form ({@code Integer},
 * {@code Long}, {@code Float}, {@code Double}, {@code Boolean}, or the
 * reference itself); struct/array values are already copied per Go's
 * value-copy rule before they reach the box. The field is deliberately
 * mutable: a closure and its loop body share the cell by reference.
 */
public final class GoBox implements GoCell {
    /** The current value in canonical boxed form. */
    public Object value;

    /**
     * The declared kind of the variable this box stores, or null when the
     * creating site did not record it (legacy behavior: {@link #set}
     * stores as-is). Pointer-typed boxes enforce the representation
     * invariant that a {@code *T} slot holds a {@code GoPointer} or null,
     * never a raw address word (see {@link #set}).
     */
    public final GoKind kind;

    public GoBox(Object value) {
        this(value, null);
    }

    public GoBox(Object value, GoKind kind) {
        this.value = value;
        this.kind = kind;
    }

    @Override
    public Object get() {
        return value;
    }

    @Override
    public void set(Object v) {
        if (kind == GoKind.POINTER && v instanceof Long l) {
            // A word store through an unsafe.Pointer view into a *T
            // variable (real reflect Set's else-branch for direct values,
            // e.g. Set(Zero) storing address 0): gc flat memory lands
            // the denoted pointer in the slot, not the raw address — a
            // raw Long(0) in the box would misread as non-nil and a
            // raw handle would leak the address representation into
            // every later use of the variable.
            value = GoPointer.pointerForAddress(l.longValue(), v);
            return;
        }
        value = v;
    }

    @Override
    public boolean sameStorageAs(GoCell other) {
        // Each boxed variable owns exactly one GoBox for its lifetime
        // (loop iterations get fresh boxes, matching gc's per-iteration
        // variables), so object identity is storage identity here.
        return this == other;
    }

    @Override
    public String toString() {
        return "GoBox{" + value + "}";
    }
}
