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

    /**
     * Storage identity (fix-redmaster-r29): two views of the same Go
     * storage are equal, even when built from distinct wrapper objects.
     * {@link graalgo.runtime.sync.GoSemaphore} keys its waiter queues by
     * the cell object, so without this a releaser holding a fresh view
     * of {@code &wg.sema} never finds the waiter's queue and the waiter
     * parks forever (26_sync_primitives deadlock: acquire enqueued under
     * one view, release looked up another). Equality mirrors
     * {@link GoPointer#sameStorageAs} exactly; the hash mirrors it per
     * storage shape (cell → the cell's storage key, element → array
     * identity + index, plain referent → referent identity, nil → 0).
     * Cell kinds that sameStorageAs never equates (cell vs element vs
     * plain) may share a hash — only equal objects must share one.
     */
    @Override
    public boolean equals(Object o) {
        return o instanceof GoPointerCell32 c && ptr.sameStorageAs(c.ptr);
    }

    @Override
    public int hashCode() {
        if (ptr.isCellPointer()) {
            graalgo.values.GoCell cell = ptr.cell();
            if (cell instanceof graalgo.frontend.GoFieldCell f) {
                return 31 * System.identityHashCode(f.ownerStruct()) + f.fieldIndex();
            }
            if (cell instanceof graalgo.frontend.GoArrayCell a) {
                return 31 * System.identityHashCode(a.array()) + a.index();
            }
            return System.identityHashCode(cell);
        }
        if (ptr.isElemPointer()) {
            return 31 * System.identityHashCode(ptr.elemArray()) + ptr.elemIndex();
        }
        if (ptr.isNil()) {
            return 0;
        }
        return System.identityHashCode(ptr.referent());
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
