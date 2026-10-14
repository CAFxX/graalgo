package graalgo.runtime.sync;

import java.util.concurrent.atomic.AtomicInteger;

/**
 * The {@code sync/atomic} 32-bit wrapper cell
 * ({@code atomic.Int32}, {@code atomic.Uint32}, {@code atomic.Bool}).
 *
 * <p>A plain {@code AtomicInteger} that additionally implements
 * {@link IntCell}, so the atomic word operations address wrapper cells
 * and arbitrary Go addresses ({@link GoPointerCell32}) through one type.
 * The inherited {@code AtomicInteger} methods satisfy the interface.
 */
public final class GoAtomicInt32 extends AtomicInteger implements IntCell {
    public GoAtomicInt32() {
        super();
    }

    public GoAtomicInt32(int initialValue) {
        super(initialValue);
    }
}
