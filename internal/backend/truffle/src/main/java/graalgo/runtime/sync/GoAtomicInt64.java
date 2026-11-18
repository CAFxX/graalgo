package graalgo.runtime.sync;

import java.util.concurrent.atomic.AtomicLong;

/**
 * The {@code sync/atomic} 64-bit wrapper cell
 * ({@code atomic.Int64}, {@code atomic.Uint64}, {@code atomic.Uintptr}).
 *
 * <p>A plain {@code AtomicLong} that additionally implements
 * {@link LongCell}, so the atomic word operations address wrapper cells
 * and arbitrary Go addresses ({@link GoPointerCell64}) through one type.
 * The inherited {@code AtomicLong} methods satisfy the interface.
 */
public final class GoAtomicInt64 extends AtomicLong implements LongCell {
    public GoAtomicInt64() {
        super();
    }

    public GoAtomicInt64(long initialValue) {
        super(initialValue);
    }
}
