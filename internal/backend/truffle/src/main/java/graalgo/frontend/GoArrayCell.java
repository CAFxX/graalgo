package graalgo.frontend;

import graalgo.values.GoCell;
import graalgo.values.GoKind;

/**
 * A {@link GoCell} addressing one slice/array element: the storage behind
 * {@code &s[i]} (P3-M9, WS D).
 *
 * <p>The cell holds the backing Java array object plus the absolute
 * element index (for slices: {@code slice.offset() + i}). If the slice
 * variable is later reassigned or re-sliced, the cell keeps pointing into
 * the original backing array — matching gc. {@code get}/{@code set} use
 * {@link Boxing#arrayGet}/{@link Boxing#arraySet}, so the canonical
 * boxed form is preserved. The value handed to {@code set} is already
 * canonical-boxed and copyForAssign'd by {@link DerefWriteNode}.
 */
public final class GoArrayCell implements GoCell {
    private final Object array;
    private final GoKind elemKind;
    private final int index;

    public GoArrayCell(Object array, GoKind elemKind, int index) {
        this.array = array;
        this.elemKind = elemKind;
        this.index = index;
    }

    @Override
    public Object get() {
        return Boxing.arrayGet(array, elemKind, index);
    }

    @Override
    public void set(Object value) {
        Boxing.arraySet(array, elemKind, index, value);
    }

    @Override
    public String toString() {
        return "GoArrayCell{index=" + index + "}";
    }

    @Override
    public boolean sameStorageAs(GoCell other) {
        // Same backing array object and same absolute element index address
        // the same storage (a re-sliced or reassigned slice variable keeps
        // pointing into the original backing array, matching gc).
        return other instanceof GoArrayCell a && array == a.array && index == a.index;
    }
}
