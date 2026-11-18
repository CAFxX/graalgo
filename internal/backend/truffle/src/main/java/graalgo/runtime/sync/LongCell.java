package graalgo.runtime.sync;

/**
 * A 64-bit atomic cell (fix-linkname, Caddy/godebug lane).
 *
 * <p>Abstracts over the two cell shapes the atomic word operations
 * address: the {@code sync/atomic} wrapper types
 * ({@link GoAtomicInt64}, a real {@code AtomicLong}) and arbitrary Go
 * addresses ({@link GoPointerCell64}, delegating to a
 * {@code GoPointer}). {@code java.util.concurrent.atomic.AtomicLong}'s
 * methods are final, so the delegating cell cannot extend it — this
 * interface is the common type {@code Atomics} operates on.
 */
public interface LongCell {
    long get();

    void set(long newValue);

    long getAndSet(long newValue);

    boolean compareAndSet(long expect, long update);

    long getAndAdd(long delta);

    long addAndGet(long delta);
}
