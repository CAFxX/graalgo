package graalgo.runtime.sync;

import graalgo.values.GoPointer;

/**
 * A 64-bit atomic cell view over an arbitrary Go address
 * (fix-linkname, Caddy/godebug lane).
 *
 * <p>Package-level {@code sync/atomic} operations
 * ({@code atomic.LoadUint64(&x)}, …) address plain Go memory, not an
 * atomic wrapper cell. The frontend's {@code "atomicaddr"} wire node
 * wraps the address's {@link GoPointer} in this delegating cell, so the
 * existing {@code AtomicOpNode} + {@code graalgo.runtime.Atomics}
 * machinery serves the op unchanged.
 *
 * <p>All operations serialize on a single shared lock: the read-modify-
 * write ops are mutually atomic, and loads/stores are mutually exclusive
 * with them. This is a scoped divergence from gc (no lock-free progress,
 * no memory-ordering subtleties) — the internal packages that need this
 * (e.g. internal/bisect's lossy dedup cache) are race-tolerant by
 * construction.
 */
public final class GoPointerCell64 implements LongCell {
    /** Shared lock (see class javadoc). */
    private static final Object LOCK = new Object();

    private final GoPointer ptr;

    public GoPointerCell64(GoPointer ptr) {
        if (ptr == null) {
            throw new IllegalArgumentException("GoPointerCell64: null pointer");
        }
        this.ptr = ptr;
    }

    private long read() {
        Object r = ptr.referent();
        if (r instanceof Number n) {
            return n.longValue();
        }
        throw new IllegalStateException(
                "GoPointerCell64: expected a numeric referent, got "
                        + (r == null ? "null" : r.getClass().getName()));
    }

    @Override
    public long get() {
        synchronized (LOCK) {
            return read();
        }
    }

    @Override
    public void set(long newValue) {
        synchronized (LOCK) {
            ptr.setReferent(newValue);
        }
    }

    @Override
    public long getAndSet(long newValue) {
        synchronized (LOCK) {
            long old = read();
            ptr.setReferent(newValue);
            return old;
        }
    }

    @Override
    public boolean compareAndSet(long expect, long update) {
        synchronized (LOCK) {
            if (read() != expect) {
                return false;
            }
            ptr.setReferent(update);
            return true;
        }
    }

    @Override
    public long getAndAdd(long delta) {
        synchronized (LOCK) {
            long old = read();
            ptr.setReferent(old + delta);
            return old;
        }
    }

    @Override
    public long addAndGet(long delta) {
        synchronized (LOCK) {
            long next = read() + delta;
            ptr.setReferent(next);
            return next;
        }
    }
}
