package graalgo.values;

import java.util.Arrays;

/**
 * A Go slice value: a descriptor into a backing array (D5).
 *
 * <p>{@code array} is the typed Java array backing the slice (e.g.
 * {@code byte[]} for {@code []uint8}, {@code short[]} for {@code []int16},
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

    /**
     * Zeroes this slice's {@code len} elements in place (the {@code clear}
     * builtin, WS G): each element is reset to its Go zero value following
     * the D5 carrier mapping. Only the {@code [offset, offset+len)} range is
     * touched; the backing array beyond the slice length is left alone. A
     * zero-length slice is a no-op.
     *
     * <p>This is the single canonical implementation of slice clearing,
     * shared by the v2 frontend {@code ClearNode} and the p3builtin
     * {@code "clear"} wire node (the earlier per-node instanceof chains
     * diverged: one missed {@code float[]}, both nulled complex/struct
     * elements).
     *
     * <p>The zero is derived from the runtime backing-array class, never
     * from the type table (the p3builtin node's documented principle):
     * primitive backings fill with 0; {@code GoComplex32[]}/{@code
     * GoComplex64[]} fill with the (0+0i) singletons — Go has no nil
     * complex, so a null slot would surface as a nil instead of the zero
     * value; {@code Object[]} slots holding {@link GoStruct}s are replaced
     * with fresh zero structs built from each element's own descriptor
     * (struct slots are never null and never shared — structs are mutable
     * values, so cleared slots must not alias one template); every other
     * reference element resets to null (Go nil).
     */
    public void clearElements() {
        if (len == 0) {
            return;
        }
        if (array instanceof byte[] ba) {
            Arrays.fill(ba, offset, offset + len, (byte) 0);
        } else if (array instanceof short[] sa) {
            Arrays.fill(sa, offset, offset + len, (short) 0);
        } else if (array instanceof int[] ia) {
            Arrays.fill(ia, offset, offset + len, 0);
        } else if (array instanceof long[] la) {
            Arrays.fill(la, offset, offset + len, 0L);
        } else if (array instanceof float[] fa) {
            Arrays.fill(fa, offset, offset + len, 0.0f);
        } else if (array instanceof double[] da) {
            Arrays.fill(da, offset, offset + len, 0.0);
        } else if (array instanceof boolean[] bo) {
            Arrays.fill(bo, offset, offset + len, false);
        } else if (array instanceof GoComplex32[] ca) {
            Arrays.fill(ca, offset, offset + len, GoComplex32.ZERO);
        } else if (array instanceof GoComplex64[] ca) {
            Arrays.fill(ca, offset, offset + len, GoComplex64.ZERO);
        } else if (array instanceof Object[] oa) {
            for (int i = offset; i < offset + len; i++) {
                Object e = oa[i];
                oa[i] = (e instanceof GoStruct gs) ? GoStruct.zero(gs.descriptor()) : null;
            }
        } else {
            throw new IllegalStateException(
                    "clear of slice with unsupported backing array: " + array.getClass());
        }
    }
}
