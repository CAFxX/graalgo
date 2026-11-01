package graalgo.runtime.sync;

import graalgo.values.GoPointer;

/**
 * A 64-bit atomic cell view over an {@code unsafe.Pointer} word
 * (fix-p0-1-atomic-pointer): the cell the package-level
 * {@code sync/atomic} LoadPointer/StorePointer lowering addresses,
 * via the {@code "atomicaddr"} wire node with cell kind {@code "ptr"}.
 *
 * <p>It is deliberately a separate class from {@link GoPointerCell64},
 * not a flag on it, because the two word types differ in their null
 * story. An {@code unsafe.Pointer} value is a long handle, and the
 * nil pointer is handle 0 — but the backend materializes an
 * uninitialized (or nil-assigned) unsafe.Pointer variable's storage
 * as Java null (the nillable-kind zero node), not as Long 0, while a
 * uint64 word's zero is Long 0. So a pointer-cell read must map null
 * to 0 (the nil pointer, exactly as the plain deref read does), while
 * a null referent under a word cell stays a loud failure there: a
 * uint64 storage holding null is corrupt state, never a legal zero.
 * Every other non-numeric referent is a loud failure here too.
 *
 * <p>All operations serialize on the shared plain-field lock
 * ({@link PlainFieldAtomicLock}) — the same lock the word cells and
 * the pointer RMW ops use — so Load/Store are mutually exclusive
 * with Swap/CAS on the same address, and a store's monitor exit
 * happens-before a later load's monitor entry (gc's acquire/release
 * ordering for the publication pattern).
 */
public final class GoUnsafePointerCell implements LongCell {
    private final GoPointer ptr;

    public GoUnsafePointerCell(GoPointer ptr) {
        if (ptr == null) {
            throw new IllegalArgumentException("GoUnsafePointerCell: null pointer");
        }
        this.ptr = ptr;
    }

    private long read() {
        Object r = ptr.referent();
        if (r == null) {
            return 0;
        }
        if (r instanceof Number n) {
            return n.longValue();
        }
        throw new IllegalStateException(
                "GoUnsafePointerCell: expected a numeric referent, got "
                        + r.getClass().getName());
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
