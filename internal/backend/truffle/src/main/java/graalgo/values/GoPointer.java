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
    // P3-M9 (WS D): cell pointer for &x where x is a variable (or field)
    // whose storage is a GoCell. referent() reads the cell's current value
    // and setReferent() writes it, so *p = v is observable through the
    // variable and vice versa. A cell pointer is never nil (the pointer
    // itself); its referent may be Java null when the cell's storage holds
    // null — e.g. an opaque sync-primitive struct field left null by
    // GoStruct.zero's carve-out (RV-P0 runtime-npe). Selector sites convert
    // a null referent to Go's nil-dereference panic, never a JVM NPE.
    private final GoCell cell;

    public GoPointer(Object referent) {
        this.referent = referent;
        this.isElemPtr = false;
        this.cell = null;
    }

    /** Creates a pointer to the storage behind a {@link GoCell} (&x, &s.f). */
    public GoPointer(GoCell cell) {
        this.cell = cell;
        this.isElemPtr = false;
        this.referent = null; // not used for cell pointers
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
        this.cell = null;
    }

    /** The pointed-to object, or {@code null} for the nil pointer. */
    public Object referent() {
        if (cell != null) {
            return cell.get();
        }
        if (isElemPtr) {
            return java.lang.reflect.Array.get(elemArray, elemIndex);
        }
        return referent;
    }

    /**
     * Sets the pointed-to object (P3-M9, WS D: {@code *p = v} assignment).
     * The pointer itself is the mutable cell; this is what makes
     * dereference writes observable through the pointer. For element
     * pointers, writes go to the array slot; for cell pointers, to the
     * variable/field storage the cell addresses.
     */
    public void setReferent(Object referent) {
        if (cell != null) {
            cell.set(referent);
            return;
        }
        if (isElemPtr) {
            java.lang.reflect.Array.set(elemArray, elemIndex, referent);
            return;
        }
        this.referent = referent;
    }

    public boolean isNil() {
        if (cell != null) {
            return false;
        }
        if (isElemPtr) {
            return elemArray == null;
        }
        return referent == null;
    }

    /**
     * True iff this is a cell pointer ({@code &x}, {@code &s.f},
     * {@code new(T)} scalars): the storage identity is the cell itself.
     * (P3-M10: {@code unsafe.Pointer} interning keys on storage identity,
     * not on the dereferenced value — see {@code UnsafeFromPtrNode}.)
     */
    public boolean isCellPointer() {
        return cell != null;
    }

    /** The cell behind a cell pointer; null unless {@link #isCellPointer}. */
    public GoCell cell() {
        return cell;
    }

    /** True iff this is an {@code &arr[i]} element pointer. */
    public boolean isElemPointer() {
        return isElemPtr;
    }

    /** The array of an element pointer; undefined unless {@link #isElemPointer}. */
    public Object elemArray() {
        return elemArray;
    }

    /** The index of an element pointer; undefined unless {@link #isElemPointer}. */
    public int elemIndex() {
        return elemIndex;
    }

    /**
     * Pointer equality (Go {@code ==} on pointers): two pointers are equal
     * iff both are nil or they address the same storage — the same cell,
     * the same array element, or the same referent object. (Comparing
     * referent <em>values</em> would conflate distinct variables holding
     * equal values.)
     */
    public boolean sameStorageAs(GoPointer o) {
        if (isNil() || o.isNil()) {
            return isNil() && o.isNil();
        }
        if (cell != null || o.cell != null) {
            // PKG-011: cell identity is storage identity, not object
            // identity — e.g. two evaluations of &s.f allocate distinct
            // GoFieldCell objects over the same (struct, field). A cell
            // pointer never addresses the same storage as a non-cell
            // pointer (heap referent or array element).
            return cell != null && o.cell != null && cell.sameStorageAs(o.cell);
        }
        if (isElemPtr || o.isElemPtr) {
            return isElemPtr && o.isElemPtr
                    && elemArray == o.elemArray && elemIndex == o.elemIndex;
        }
        return referent == o.referent;
    }

    @Override
    public String toString() {
        if (cell != null) {
            return "GoPointer{cell " + cell + "}";
        }
        if (isElemPtr) {
            return "GoPointer{&arr[" + elemIndex + "]}";
        }
        return isNil() ? "GoPointer{nil}" : "GoPointer{" + referent + "}";
    }
}
