package graalgo.sched;

import java.util.concurrent.atomic.AtomicLong;

/**
 * Per-goroutine scheduler handle: the virtual thread plus the park/unpark
 * ticket state (P2-D7, spike 2b §1.2).
 *
 * <p>Ticket discipline: every park reserves a generation via
 * {@link SchedulerCore#armPark} BEFORE the waiter becomes visible to any
 * waker. A waker admits generation {@code t} by setting {@link #acked} to
 * {@code t} and <em>then</em> unparking — ack-before-unpark (GG-WAKE-0001);
 * reversing the order loses wakeups. The waiter spins on
 * {@code acked >= t} with {@link java.util.concurrent.locks.LockSupport#park()},
 * so an early unpark is absorbed by the permit and spurious wakeups re-park.
 */
public final class GoroutineHandle {
    private static final AtomicLong ID_GEN = new AtomicLong(1);

    /** Stable id, for logs and tests. */
    public final long id = ID_GEN.getAndIncrement();

    /** The virtual thread running this goroutine; set before start. */
    volatile Thread thread;

    /** Next park generation to reserve. */
    final AtomicLong parkSeq = new AtomicLong(0);

    /**
     * Currently armed generation, or -1 when not parked. A waker wins
     * generation {@code t} via {@code armed.compareAndSet(t, -2)}.
     */
    final AtomicLong armed = new AtomicLong(-1);

    /** Highest generation acknowledged (admitted) by the drainer. */
    final AtomicLong acked = new AtomicLong(-1);

    /** Next quantum deadline, nanoTime. Reset on every admission. */
    volatile long quantumDeadlineNanos;

    /**
     * Wake action for {@code parkCurrent(Runnable)}; run by the waker
     * thread before the WAKE token is enqueued. Null when not parked
     * via park/unpark.
     */
    volatile Runnable parkWakeAction;

    /** Body executed by the vthread. */
    final Runnable body;

    GoroutineHandle(Runnable body) {
        this.body = body;
    }
}
