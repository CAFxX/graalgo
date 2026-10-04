package graalgo.values;

/**
 * A mutable cell holding one Go value in canonical boxed form
 * (M6, per-iteration capture).
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
 *   <li>{@code for}-header variables ({@code for i := 0; ...}): two cells —
 *       an <em>outer</em> cell for init/cond/post plus closures created in
 *       the header, and a per-iteration cell for the body, refreshed from
 *       the outer cell at the top of every iteration. The per-iteration
 *       cell's {@link #outer} points at the outer cell, so body writes
 *       (including writes through captured closures) go through to the
 *       outer cell and post/cond observe body mutations exactly as gc
 *       does (gc-oracle verified: t2 body-mutate exits after one
 *       iteration, t10 cond-closure sees the post's increment, t11
 *       closure write is visible to the post).</li>
 * </ul>
 *
 * <p>{@code value} is the canonical boxed form ({@code Integer},
 * {@code Long}, {@code Float}, {@code Double}, {@code Boolean}, or the
 * reference itself); struct/array values are already copied per Go's
 * value-copy rule before they reach the box. The field is deliberately
 * mutable: a closure and its loop body share the cell by reference.
 */
public final class GoBox {
    /** The current value in canonical boxed form. */
    public Object value;
    /**
     * The outer cell for a {@code for}-header variable's per-iteration
     * body cell (write-through target); null for every other boxed
     * variable and for outer cells themselves.
     */
    public final GoBox outer;

    public GoBox(Object value) {
        this(value, null);
    }

    public GoBox(Object value, GoBox outer) {
        this.value = value;
        this.outer = outer;
    }

    @Override
    public String toString() {
        return "GoBox{" + value + (outer == null ? "" : ", outer") + "}";
    }
}
