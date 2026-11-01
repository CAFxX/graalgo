package graalgo.runtime.sync;

/**
 * The single monitor for every atomic operation on a plain Go field
 * (fix-p0-1-atomic-pointer): the {@link GoPointerCell64} word cells, the
 * {@code atomicptrop} pointer RMW ops, and — through those cells — the
 * package-level {@code sync/atomic} LoadPointer/StorePointer lowering.
 *
 * <p>All of these serialize on this one object (never on a per-cell or
 * per-op lock) so that operations on the same address are mutually
 * exclusive <em>with each other</em>: a LoadPointer that follows a
 * SwapPointer on the same field, or a StoreUint64 that follows a
 * StorePointer, cannot observe a stale value merely because the two
 * ops locked different monitors. Monitor entry/exit on this lock is
 * also what gives the plain-field ops their ordering: a store's
 * monitor exit happens-before a later load's monitor entry, so the
 * storing goroutine's prior plain writes are visible to the goroutine
 * whose atomic load observes the stored value — gc's acquire/release
 * semantics for the publication pattern, for ops that go through this
 * lock. (AtomicReference cells, used for atomic wrapper values, get
 * the same guarantee from volatile access instead.)
 *
 * <p>No lock-free progress guarantee is made: this is a correctness
 * mechanism, not a performance one.
 */
public final class PlainFieldAtomicLock {
    /** The shared monitor. Never expose a second one. */
    public static final Object LOCK = new Object();

    private PlainFieldAtomicLock() {
    }
}
