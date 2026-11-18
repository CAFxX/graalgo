package graalgo.runtime.sync;

import graalgo.values.GoCell;
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

    /**
     * Storage identity (fix-redmaster-r43): two cells address the same
     * {@code *uint32} iff their pointers address the same storage
     * ({@link GoPointer#sameStorageAs}). Required by {@code GoSemaphore},
     * which keys waiter queues by the cell: a fresh cell per intrinsic
     * call (e.g. every {@code &m.sema} evaluation allocates a new
     * {@link graalgo.frontend.GoFieldCell}) must still find the same
     * queue, or a waiter parks forever. The hash decomposes the
     * storage the same way {@code sameStorageAs} compares it, so equal
     * cells hash equally: field cells by (owner struct identity, field
     * index), array cells by (backing array identity, index), element
     * pointers by (array identity, index), other cells and referent
     * pointers by object identity.
     */
    @Override
    public boolean equals(Object o) {
        if (!(o instanceof GoPointerCell32 other)) {
            return false;
        }
        return ptr.sameStorageAs(other.ptr);
    }

    @Override
    public int hashCode() {
        GoCell c = ptr.cell();
        if (c instanceof graalgo.frontend.GoFieldCell f) {
            return System.identityHashCode(f.ownerStruct()) * 31 + f.fieldIndex();
        }
        if (c instanceof graalgo.frontend.GoArrayCell a) {
            return System.identityHashCode(a.array()) * 31 + a.index();
        }
        if (c != null) {
            return System.identityHashCode(c);
        }
        if (ptr.isElemPointer()) {
            return System.identityHashCode(ptr.elemArray()) * 31 + ptr.elemIndex();
        }
        Object r = ptr.referent();
        return r == null ? 0 : System.identityHashCode(r);
    }
}
