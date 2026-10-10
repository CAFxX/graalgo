package graalgo.values;

/**
 * A Go pointer value (M4, WS E): an opaque reference to a referent object.
 *
 * <p>Per PLAN.md D5, pointers are opaque Java references; this holder is
 * the Phase-1 representation. A {@code null} referent is the nil pointer.
 * Pointers arise in M4 from {@code &T{...}} and from the implicit
 * {@code &T{}} element elision in composite literals (e.g.
 * {@code []*Point{{1, 2}}}); explicit {@code &x} / {@code *p} stay WS D.
 * Selectors auto-deref through non-nil pointers (nil → gc's nil-dereference
 * panic, M5 WS F).
 */
public final class GoPointer {
    private Object referent;

    public GoPointer(Object referent) {
        this.referent = referent;
    }

    /** The pointed-to object, or {@code null} for the nil pointer. */
    public Object referent() {
        return referent;
    }

    /**
     * Sets the pointed-to object (P3-M9, WS D: {@code *p = v} assignment).
     * The pointer itself is the mutable cell; this is what makes
     * dereference writes observable through the pointer.
     */
    public void setReferent(Object referent) {
        this.referent = referent;
    }

    public boolean isNil() {
        return referent == null;
    }

    @Override
    public String toString() {
        return isNil() ? "GoPointer{nil}" : "GoPointer{" + referent + "}";
    }
}
