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
    // P3-M9 (WS D): element pointer for &arr[i]. When non-null, the pointer
    // refers to array[index]; referent() reads and setReferent() writes the
    // array slot (so *p = v updates arr[i], not a copy).
    private Object elemArray;
    private int elemIndex;
    private boolean isElemPtr;

    public GoPointer(Object referent) {
        this.referent = referent;
        this.isElemPtr = false;
    }

    /**
     * Creates a pointer to an array/slice element (P3-M9, WS D: {@code &arr[i]}).
     * The array is the Java array object (Object[] or primitive array);
     * index is the element index.
     */
    public GoPointer(Object array, int index) {
        this.elemArray = array;
        this.elemIndex = index;
        this.isElemPtr = true;
        this.referent = null; // not used for element pointers
    }

    /** The pointed-to object, or {@code null} for the nil pointer. */
    public Object referent() {
        if (isElemPtr) {
            return java.lang.reflect.Array.get(elemArray, elemIndex);
        }
        return referent;
    }

    /**
     * Sets the pointed-to object (P3-M9, WS D: {@code *p = v} assignment).
     * The pointer itself is the mutable cell; this is what makes
     * dereference writes observable through the pointer. For element
     * pointers, writes go to the array slot.
     */
    public void setReferent(Object referent) {
        if (isElemPtr) {
            java.lang.reflect.Array.set(elemArray, elemIndex, referent);
            return;
        }
        this.referent = referent;
    }

    public boolean isNil() {
        if (isElemPtr) {
            return elemArray == null;
        }
        return referent == null;
    }

    @Override
    public String toString() {
        if (isElemPtr) {
            return "GoPointer{&arr[" + elemIndex + "]}";
        }
        return isNil() ? "GoPointer{nil}" : "GoPointer{" + referent + "}";
    }
}
