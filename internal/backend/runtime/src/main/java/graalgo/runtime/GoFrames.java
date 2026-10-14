package graalgo.runtime;

/**
 * The opaque handle behind a Go {@code *runtime.Frames} value: the
 * interned PCs handed to {@code runtime.CallersFrames} plus the read
 * cursor for {@code Next}.
 *
 * <p>gc: "The more result indicates whether the next call to Next will
 * return a valid Frame." The cursor model implements exactly that: each
 * {@link #next} consumes one PC, and {@link #hasNext} reports whether a
 * subsequent {@code Next} would yield a frame.
 */
public final class GoFrames {
    private final long[] pcs;
    private int cursor;

    public GoFrames(long[] pcs) {
        this.pcs = pcs.clone();
    }

    /** True when a subsequent {@code Next} would return a valid frame. */
    public boolean hasNext() {
        return cursor < pcs.length;
    }

    /** Consumes and returns the next PC. Call only when {@link #hasNext}. */
    public long next() {
        return pcs[cursor++];
    }

    /**
     * Consumes and returns the next PC that resolves to a known function,
     * or -1 when exhausted. gc's {@code Frames.Next} skips PCs that
     * {@code findfunc} cannot resolve (e.g. cgo frames without a
     * symbolizer); the interned-PC analogue is a lookup miss.
     */
    public long nextValid() {
        while (cursor < pcs.length) {
            long pc = pcs[cursor++];
            if (StackTrace.lookup(pc) != null) {
                return pc;
            }
        }
        return -1;
    }

    /**
     * True when a subsequent {@link #nextValid} would return a PC — the
     * exact meaning of gc's {@code more} result ("whether the next call
     * to Next will return a valid Frame").
     */
    public boolean hasValidNext() {
        for (int i = cursor; i < pcs.length; i++) {
            if (StackTrace.lookup(pcs[i]) != null) {
                return true;
            }
        }
        return false;
    }
}
