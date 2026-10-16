package graalgo.sched;

import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Per-goroutine scheduler handle: the virtual thread plus the park/unpark
 * ticket state (P2-D7, spike 2b §1.2).
 *
 * <p>Ticket discipline: every park reserves a generation via
 * {@link SchedulerCore#armPark} BEFORE the waiter becomes visible to any
 * waker, so a generation always identifies one specific park. Wakeup
 * matching is done by the single drainer thread (PARKED/UNPARK tokens
 * matched by handle, tolerating both orders) — no concurrent waker ever
 * races the drainer in P2-M1. Every admission does ack-before-unpark
 * (GG-WAKE-0001): the drainer sets {@link #acked} to the generation and
 * <em>then</em> unparks; reversing the order loses wakeups. The waiter
 * spins on {@code acked >= t} with
 * {@link java.util.concurrent.locks.LockSupport#park()}, so an early
 * unpark is absorbed by the permit and spurious wakeups re-park.
 *
 * <p>The {@code armed.compareAndSet(t, -2)} external-waker protocol from
 * spike 2b applies when peer goroutines (channels, P2-M2) can win
 * wakeups directly; P2-M1 routes all wakeups through the drainer.
 */
public final class GoroutineHandle {
    private static final AtomicLong ID_GEN = new AtomicLong(1);

    /** Stable id, for logs and tests. */
    public final long id = ID_GEN.getAndIncrement();

    /** The virtual thread running this goroutine; set before start. */
    public volatile Thread thread;

    /** Next park generation to reserve. */
    public final AtomicLong parkSeq = new AtomicLong(0);

    /**
     * Currently armed generation, or -1 when not parked. Set by
     * {@code armPark} before the waiter becomes visible; the drainer
     * matches PARKED/UNPARK tokens by handle against this generation.
     * (The {@code compareAndSet(t, -2)} external-waker protocol from
     * spike 2b is not implemented in P2-M1; all wakeups go through the
     * drainer — see class javadoc.)
     *
     * <p>Intentionally retained as the test-visible park signal:
     * {@code SchedulerCoreTest} polls it to know a goroutine is actually
     * parked. No production code reads it.
     */
    final AtomicLong armed = new AtomicLong(-1);

    /** Highest generation acknowledged (admitted) by the drainer. */
    final AtomicLong acked = new AtomicLong(-1);

    /**
     * JFR park tracking (P2-M6, WS F): reason + start nanos for the
     * current park, set by {@code JfrEvents.notePark} before the park
     * becomes visible, consumed by {@code JfrEvents.parkResumed} on
     * resume. {@code null} reason = not a tracked park (initial
     * admission).
     */
    public volatile graalgo.jfr.ParkReason parkReason;
    public volatile long parkStartNanos;

    /**
     * JFR I/O tracking (P2-M6): nanos at {@code ioEnter}, for the full
     * syscall-duration park event emitted at {@code ioExit}.
     */
    public volatile long ioStartNanos;

    /**
     * JFR I/O interval (P2-M6, reviewer2 P2-7): event begun at
     * {@code ioEnter} (null when disabled at enter), committed at
     * {@code ioExit} so the JFR interval spans the syscall.
     */
    public volatile graalgo.jfr.GoroutineParkEvent ioParkEvent;

    /**
     * JFR lifetime tracking (P2-M6): nanos when the goroutine started
     * executing (after first admission); read in the finally block for
     * {@code graalgo.GoroutineEnd}.
     */
    public volatile long startNanos;

    /** Next quantum deadline, nanoTime. Reset on every admission. */
    volatile long quantumDeadlineNanos;

    /**
     * Wake action for {@code parkCurrent(Runnable)}; runs on the woken
     * goroutine's own vthread immediately after resume. Set before the
     * PARKED token is offered; consumed on resume.
     */
    final AtomicReference<Runnable> wakeAction = new AtomicReference<>();

    /**
     * Blocking-I/O flag (P2-M3, GG-0018): set by {@code ioEnter},
     * cleared by {@code ioExit}, on the goroutine's own vthread.
     * Calling-thread fail-fast for re-entrant ioEnter / unpaired ioExit;
     * the drainer's in-I/O set is the authoritative accounting.
     */
    final java.util.concurrent.atomic.AtomicBoolean inIo = new java.util.concurrent.atomic.AtomicBoolean();

    /**
     * Set when the goroutine's body has completed (P1-10): written once
     * in {@link SchedulerCore#spawn}'s finally block, before the EXIT
     * token is offered, and read by the drainer. Lets the drainer tell a
     * stale UNPARK (unpark racing goroutine exit) from a legitimate
     * pre-park wakeup, so the stale one is dropped instead of being
     * retained in {@code unparkPending} forever.
     */
    public volatile boolean exited;

    /** Body executed by the vthread. */
    final Runnable body;

    GoroutineHandle(Runnable body) {
        this.body = body;
    }
}
