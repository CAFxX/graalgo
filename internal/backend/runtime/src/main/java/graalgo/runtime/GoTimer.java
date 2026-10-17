package graalgo.runtime;

import graalgo.sched.SchedulerCore;

/**
 * Go timer/ticker (P2-M3, WS C; GG-SCHED-0004).
 *
 * <p>Models {@code time.Timer} / {@code time.Ticker}: a timer channel
 * (physical 1-slot buffer for the fire nanotime, observed as unbuffered
 * per gc — see {@link TimerChan}) that receives the fire nanotime.
 * Stop/Reset follow gc semantics exactly (verified differentially vs
 * gc 1.27.1):
 * <ul>
 *   <li>{@link #stop()} returns true iff the timer was stopped before
 *       firing; false if it already fired or was stopped. It does not
 *       close the channel. Go 1.23+ parity: a fired-but-undrained timer
 *       counts as active — Stop drains the pending value and returns
 *       true (verified vs gc 1.27.1).
 *   <li>{@link #reset(long)} re-arms for {@code ns} from now, returning
 *       true iff the timer had been active, where expired-undrained
 *       counts as active (its stale value is drained). Go 1.23+ parity.
 *   <li>Slow ticker receivers drop ticks (channel full → fire is a
 *       no-op); a ticker more than a period behind skips ahead instead
 *       of bursting.
 *   <li>{@code newTicker} with non-positive interval panics, gc message
 *       parity ("non-positive interval for NewTicker").
 * </ul>
 *
 * <p>Stop/Reset synchronize via the timer heap (heap lock → owner lock
 * order, never the reverse); concurrent Stop/Reset on the same timer
 * from multiple goroutines is serialized per-timer.
 *
 * <p>The channel element is the fire nanotime boxed as {@link Long}
 * (opaque until Phase 3 brings {@code time.Time}).
 *
 * <p><b>Resolved divergence (P3-M2a; was P2-1/DIV-M3-1):</b> gc
 * presents timer channels as unbuffered — {@code len(t.C)} and
 * {@code cap(t.C)} always report 0. The timer channel is now the
 * dedicated {@link TimerChan} flavor of {@link GoChan}: the real
 * 1-slot buffer underneath keeps fire values deliverable (recv,
 * select, close-drain) while the observation probes are masked to 0.
 * The {@code DIV-M3-1} re-test trigger is discharged by the differential
 * timer corpus (tm15) re-run.
 *
 * <p>verifies: GG-SCHED-0004 (timer slice)
 */
public final class GoTimer implements TimerHeap.ChanOwner {
    private final TimerHeap heap;
    private final TimerChan chan;
    private final long periodNanos; // 0 → one-shot timer
    // Guarded by heap-lock → this-lock order (see TimerHeap).
    private long id = -1;
    private long generation;

    private GoTimer(TimerHeap heap, long ns, long periodNanos) {
        this.heap = heap;
        this.chan = new TimerChan();
        this.periodNanos = periodNanos;
        long deadline = System.nanoTime() + Math.max(0, ns);
        // P1-1: addChan assigns this.id under heap→owner locks before
        // the entry is visible; no this-escape before id assignment.
        heap.addChan(deadline, chan, this, periodNanos, generation);
    }

    /**
     * {@code time.NewTimer(d)}: fires once after {@code ns}
     * nanoseconds. Non-positive {@code ns} fires immediately.
     */
    public static GoTimer newTimer(long ns) {
        TimerHeap heap = SchedulerCore.current().timerHeap();
        return new GoTimer(heap, ns, 0);
    }

    /**
     * {@code time.NewTicker(d)}: fires every {@code ns} nanoseconds.
     * Panics on non-positive interval (gc parity).
     */
    public static GoTimer newTicker(long ns) {
        if (ns <= 0) {
            Panic.throwPanic(GoStrings.of("non-positive interval for NewTicker"));
        }
        TimerHeap heap = SchedulerCore.current().timerHeap();
        return new GoTimer(heap, ns, ns);
    }

    /**
     * {@code time.After(d)}: the channel of a one-shot timer.
     * Non-positive {@code ns} fires immediately.
     */
    public static ChanOps.Chan after(long ns) {
        return newTimer(ns).channel();
    }

    /**
     * {@code time.Tick(d)}: the channel of a ticker (no Stop handle —
     * the ticker leaks until the program ends, gc parity). Returns null
     * on non-positive interval (gc parity: unlike NewTicker, Tick
     * returns nil if d <= 0).
     */
    public static ChanOps.Chan tick(long ns) {
        if (ns <= 0) {
            return null;
        }
        return newTicker(ns).channel();
    }

    /** The timer channel ({@link TimerChan}: physically 1-buffered for the fire value, observed as unbuffered). */
    public ChanOps.Chan channel() {
        return chan;
    }

    /** Package-private: the concrete channel for the heap. */
    GoChan channelImpl() {
        return chan;
    }

    /**
     * {@code (*Timer).Stop} / {@code (*Ticker).Stop}: prevents further
     * fires. Returns true iff the timer was active.
     */
    public boolean stop() {
        return heap.stopChan(this);
    }

    /**
     * {@code (*Timer).Reset} / {@code (*Ticker).Reset}: re-arms for
     * {@code ns} nanoseconds from now. Returns true iff the timer had
     * been active.
     *
     * <p>gc parity: {@code Ticker.Reset} panics on non-positive
     * interval ("non-positive interval for Ticker.Reset");
     * {@code Timer.Reset} accepts it (fires immediately).
     */
    public boolean reset(long ns) {
        if (periodNanos > 0 && ns <= 0) {
            Panic.throwPanic(GoStrings.of("non-positive interval for Ticker.Reset"));
        }
        return heap.resetChan(this, System.nanoTime() + Math.max(0, ns));
    }

    // ---- TimerHeap.ChanOwner ----

    @Override
    public long periodNanos() {
        return periodNanos;
    }

    @Override
    public boolean stillCurrent(long id, long generation) {
        synchronized (this) {
            return this.id == id && this.generation == generation;
        }
    }

    // ---- package-private state for TimerHeap (heap lock → this lock) ----

    long currentId() {
        return id;
    }

    long generationLocked() {
        return generation;
    }

    void setIdLocked(long id) {
        this.id = id;
    }

    void clearIdLocked() {
        this.id = -1;
    }

    void bumpGenerationLocked() {
        generation++;
    }
}
