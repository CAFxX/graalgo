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
 * <p>All operations serialize on the shared plain-field lock
 * ({@link PlainFieldAtomicLock}): the read-modify-write ops are mutually
 * atomic, and loads/stores are mutually exclusive with them — and with
 * the pointer RMW ops and the LoadPointer/StorePointer lowering, which
 * share the same lock. Monitor exit on a store happens-before monitor
 * entry on a later load of the same address, which is what gives these
 * ops gc's acquire/release ordering for the publication pattern. This
 * is a scoped divergence from gc only in that there is no lock-free
 * progress guarantee.
 */
public final class GoPointerCell64 implements LongCell {
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
        synchronized (PlainFieldAtomicLock.LOCK) {
            return read();
        }
    }

    @Override
    public void set(long newValue) {
        synchronized (PlainFieldAtomicLock.LOCK) {
            ptr.setReferent(newValue);
        }
    }

    @Override
    public long getAndSet(long newValue) {
        synchronized (PlainFieldAtomicLock.LOCK) {
            long old = read();
            ptr.setReferent(newValue);
            return old;
        }
    }

    @Override
    public boolean compareAndSet(long expect, long update) {
        synchronized (PlainFieldAtomicLock.LOCK) {
            if (read() != expect) {
                return false;
            }
            ptr.setReferent(update);
            return true;
        }
    }

    @Override
    public long getAndAdd(long delta) {
        synchronized (PlainFieldAtomicLock.LOCK) {
            long old = read();
            ptr.setReferent(old + delta);
            return old;
        }
    }

    @Override
    public long addAndGet(long delta) {
        synchronized (PlainFieldAtomicLock.LOCK) {
            long next = read() + delta;
            ptr.setReferent(next);
            return next;
        }
    }
}
