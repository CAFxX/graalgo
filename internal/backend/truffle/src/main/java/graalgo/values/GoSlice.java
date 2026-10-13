package graalgo.values;

/**
 * A Go slice value: a descriptor into a backing array (D5).
 *
 * <p>{@code array} is the typed Java array backing the slice (e.g.
 * {@code int[]} for {@code []int32}, {@code long[]} for {@code []int64},
 * {@code Object[]} for reference element types); {@code offset} is the
 * start index within it; {@code len}/{@code cap} are the Go length and
 * capacity. Slices share backing arrays on slicing/subslicing (no copy);
 * {@code append} may allocate a new array (WS G owns the growth policy).
 */
public final class GoSlice {
    private final Object array;
    private final int offset;
    private final int len;
    private final int cap;

    public GoSlice(Object array, int offset, int len, int cap) {
        if (array == null) {
            throw new IllegalArgumentException("backing array must not be null (use a nil GoSlice reference for nil slices)");
        }
        if (offset < 0 || len < 0 || cap < 0 || offset + len > arrayLength(array) || len > cap
                || offset + cap > arrayLength(array)) {
            throw new IllegalArgumentException(
                    "slice bounds out of range: offset=" + offset + " len=" + len + " cap=" + cap);
        }
        this.array = array;
        this.offset = offset;
        this.len = len;
        this.cap = cap;
    }

    private static int arrayLength(Object array) {
        return java.lang.reflect.Array.getLength(array);
    }

    public Object array() {
        return array;
    }

    public int offset() {
        return offset;
    }

    public int len() {
        return len;
    }

    public int cap() {
        return cap;
    }

    /** Subslice {@code s[lo:hi]}: shares the backing array (no copy). */
    public GoSlice slice(int lo, int hi) {
        return slice3(lo, hi, cap);
    }

    /** Three-index slice {@code s[lo:hi:max]}: shares the backing array. */
    public GoSlice slice3(int lo, int hi, int max) {
        if (lo < 0 || hi < lo || max < hi || max > cap) {
            throw new IndexOutOfBoundsException(
                    "slice bounds out of range: [" + lo + ":" + hi + ":" + max + "] on len=" + len);
        }
        return new GoSlice(array, offset + lo, hi - lo, max - lo);
    }

    @Override
    public String toString() {
        return "GoSlice{len=" + len + ", cap=" + cap + "}";
    }
}
