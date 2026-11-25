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

    /** The thread running this goroutine (thread mode only; null on carriers); set before start. */
    public volatile Thread thread;

    // ---- carrier mode (Carriers, ADR-0018) ----

    /** The goroutine's continuation (a {@code jdk.internal.vm.Continuation}); null once done. */
    Object cont;
    /** Run state ({@code Carriers.RUNNING} ...), accessed through a VarHandle. */
    volatile int runState;
    /** Pending unpark permit, accessed through a VarHandle. */
    volatile boolean permit;
    /** The carrier the goroutine last ran on (its pinned park blocks that thread). */
    Carriers.Carrier carrier;
    /** The goroutine's {@code GoroutineState} holder value while unmounted. */
    graalgo.runtime.GoroutineState savedState;
    /** The goroutine's node-package {@code G} while unmounted. */
    graalgo.go.runtime.G savedG;
    /** The scheduler's live list (carrier mode; guarded by its quiescence lock). */
    GoroutineHandle liveNext;
    GoroutineHandle livePrev;

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
     * Direct handoff (WP-SCHED, gc's runnext): set by a waker that hands
     * this goroutine its in-flight slot as it parks itself
     * ({@code SchedulerCore.parkCurrent}); consumed by this goroutine's
     * pending {@code parkCurrent}, which it admits in place of the drainer's
     * ack. Set BEFORE the waker's {@code LockSupport.unpark} (GG-WAKE-0001's
     * ack-before-unpark).
     */
    final java.util.concurrent.atomic.AtomicBoolean handoff =
            new java.util.concurrent.atomic.AtomicBoolean();

    /**
     * The goroutine this one woke and has not yet released (gc's runnext):
     * its wake is deferred so that, if this goroutine parks next, it hands
     * its slot to that goroutine directly instead of two drainer round
     * trips. Owner thread only. Flushed as an ordinary UNPARK on every other
     * scheduler interaction (quantum park, I/O enter, exit, a second wake)
     * and by the first full poll after {@code SchedulerCore#HANDOFF_WINDOW_NANOS}.
     */
    GoroutineHandle runnext;

    /** nanoTime when {@link #runnext} was set (owner thread only). */
    long runnextSinceNanos;

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
     * Depth of {@link SchedulerCore#disablePreemption} sections (gc's {@code m.locks}). Only the
     * goroutine itself reads or writes it, on its own thread.
     */
    int preemptOff;

    /**
     * Wake action for {@code parkCurrent(Runnable)}; runs on the woken
     * goroutine's own vthread immediately after resume. Set before the
     * PARKED token is offered; consumed on resume.
     */
    final AtomicReference<Runnable> wakeAction = new AtomicReference<>();

    /**
     * Blocking-I/O flag (P2-M3, GG-0063): set by {@code ioEnter},
     * cleared by {@code ioExit}, on the goroutine's own vthread.
     * Calling-thread fail-fast for re-entrant ioEnter / unpaired ioExit;
     * the drainer's in-I/O set is the authoritative accounting.
     */
    final java.util.concurrent.atomic.AtomicBoolean inIo = new java.util.concurrent.atomic.AtomicBoolean();

    /** Carrier mode: between ioEnter's {@code beginBlocking} and its end (owner thread only). */
    boolean ioBlocking;

    /**
     * gc's wait reason for the current scheduler park (e.g.
     * {@code "chan receive"}, {@code "sync.Mutex.Lock"}), rendered in the
     * {@code goroutine N [reason]:} header of a deadlock dump. Written by
     * the goroutine itself before it offers its PARKED token, so the
     * drainer (which reads it only for parked goroutines) observes it via
     * the inbox's happens-before edge.
     */
    volatile String waitReason;

    /**
     * True while the goroutine is parked waiting on a waker that is not a
     * goroutine and not the timer heap (the netpoller's selector thread):
     * such a park can be ended from outside the scheduler, so it never
     * counts toward deadlock (gc: a goroutine in netpoll keeps an M
     * blocked in the poller, so checkdead never fires).
     */
    volatile boolean externalWake;

    /**
     * Runtime-internal goroutine (gc's {@code isSystemGoroutine}): e.g.
     * the time package's timer translators. Excluded from deadlock dumps
     * and from the user-goroutine count that decides deadlock.
     */
    volatile boolean system;

    /** True for the program's main goroutine (exit on return). */
    volatile boolean isMain;

    /**
     * The goroutine's {@link graalgo.runtime.GoroutineState} (set on its
     * own vthread at start): the deadlock dump reads the Go stack of
     * parked goroutines through it.
     */
    volatile graalgo.runtime.GoroutineState state;

    /** Creator goroutine id (0 when spawned from outside a goroutine). */
    volatile long creatorId;

    /** Creator's innermost Go frame name at the {@code go} statement, or null. */
    volatile String creatorFunc;

    /** Creator frame's call-site section, captured at spawn time. */
    volatile com.oracle.truffle.api.source.SourceSection creatorSection;

    /** Body executed by the vthread. */
    final Runnable body;

    GoroutineHandle(Runnable body) {
        this.body = body;
    }
}
