package graalgo.runtime.sync;

import graalgo.values.GoPointer;

/**
 * A 32-bit atomic cell view over an arbitrary Go address
 * (fix-linkname, Caddy/godebug lane).
 *
 * <p>The 32-bit counterpart of {@link GoPointerCell64}: serves
 * {@code atomic.LoadInt32(&x)} and {@code atomic.LoadUint32(&x)}.
 * See {@link GoPointerCell64} for the locking and divergence notes.
 */
public final class GoPointerCell32 implements IntCell {
    /** Shared lock (see GoPointerCell64). */
    private static final Object LOCK = new Object();

    private final GoPointer ptr;

    public GoPointerCell32(GoPointer ptr) {
        if (ptr == null) {
            throw new IllegalArgumentException("GoPointerCell32: null pointer");
        }
        this.ptr = ptr;
    }

    private int read() {
        Object r = ptr.referent();
        if (r instanceof Number n) {
            return n.intValue();
        }
        throw new IllegalStateException(
                "GoPointerCell32: expected a numeric referent, got "
                        + (r == null ? "null" : r.getClass().getName()));
    }

    @Override
    public int get() {
        synchronized (LOCK) {
            return read();
        }
    }

    @Override
    public void set(int newValue) {
        synchronized (LOCK) {
            ptr.setReferent(newValue);
        }
    }

    @Override
    public int getAndSet(int newValue) {
        synchronized (LOCK) {
            int old = read();
            ptr.setReferent(newValue);
            return old;
        }
    }

    @Override
    public boolean compareAndSet(int expect, int update) {
        synchronized (LOCK) {
            if (read() != expect) {
                return false;
            }
            ptr.setReferent(update);
            return true;
        }
    }

    @Override
    public int getAndAdd(int delta) {
        synchronized (LOCK) {
            int old = read();
            ptr.setReferent(old + delta);
            return old;
        }
    }

    @Override
    public int addAndGet(int delta) {
        synchronized (LOCK) {
            int next = read() + delta;
            ptr.setReferent(next);
            return next;
        }
    }
}
