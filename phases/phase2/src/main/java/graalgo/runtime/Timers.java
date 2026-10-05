package graalgo.runtime;

import graalgo.sched.SchedulerCore;

/**
 * Timer intrinsic surface (WS G, GG-0020; WS C, GG-SCHED-0004).
 *
 * <p><strong>Phase-2 implementation</strong> (P2-D8): this class
 * <em>replaces</em> the Phase-1 stub of the same fully-qualified name.
 * Signatures are byte-identical to the frozen ones (bodies are INTERNAL
 * per freeze §3.5); {@code FreezeSignatureTest} keeps passing.
 *
 * <p>All blocking parks scheduler-aware via
 * {@link SchedulerCore#parkCurrent} — a sleeping goroutine never pins
 * a carrier thread and does not count against the in-flight cap while
 * parked (P2-D4).
 *
 * <p>verifies: GG-0020 (timer slice), GG-SCHED-0004 (timer slice)
 */
public final class Timers {
    private Timers() {}

    /**
     * Monotonic nanosecond clock (REAL): {@code runtime.nanotime}.
     * Never wall-clock; never goes backwards on this platform.
     */
    public static long nanotime() {
        return System.nanoTime();
    }

    /**
     * Sleep for {@code ns} nanoseconds (frozen signature, now real).
     * Parks the goroutine on the scheduler timer heap; the in-flight
     * slot is released while sleeping. {@code ns <= 0} returns
     * immediately (gc {@code time.Sleep} parity).
     */
    public static void sleep(long ns) {
        if (ns <= 0) {
            return;
        }
        SchedulerCore sched = SchedulerCore.current();
        // Register BEFORE parking (P2-D7): the timer thread's unpark is
        // safe even if it wins the race (drainer unparkPending absorbs
        // unpark-before-parked).
        sched.timerHeap().addSleep(
                System.nanoTime() + ns, sched.currentGoroutine());
        sched.parkCurrent(null);
    }

    /**
     * Arm a timer for an absolute nanotime (frozen signature, now
     * real). At {@code whenNanos}, {@code callback} runs on the timer
     * thread (gc timer-proc parity): it must not block and must not
     * call scheduler park operations. The timer id is discarded; use
     * {@link #startTimerId} when cancellation is needed.
     */
    public static void startTimer(long whenNanos, Runnable callback) {
        startTimerId(whenNanos, callback);
    }

    /**
     * Additive (Phase 2): like {@link #startTimer} but returns the
     * timer id for {@link #stopTimer}.
     */
    public static long startTimerId(long whenNanos, Runnable callback) {
        if (callback == null) {
            throw new IllegalArgumentException("startTimer: null callback");
        }
        return SchedulerCore.current().timerHeap().addCallback(whenNanos, callback);
    }

    /**
     * Disarm a timer (frozen signature, now real). Returns silently
     * when the id is unknown or the timer already fired (gc Stop
     * parity for the racy window).
     */
    public static void stopTimer(long timerId) {
        SchedulerCore.current().timerHeap().cancel(timerId);
    }

    /**
     * Additive (Phase 2): {@code time.After(d)} — the channel of a
     * one-shot timer firing after {@code ns} nanoseconds.
     */
    public static ChanOps.Chan after(long ns) {
        return GoTimer.after(ns);
    }

    /**
     * Additive (Phase 2): {@code time.NewTimer(d)}.
     */
    public static GoTimer newTimer(long ns) {
        return GoTimer.newTimer(ns);
    }

    /**
     * Additive (Phase 2): {@code time.NewTicker(d)}.
     */
    public static GoTimer newTicker(long ns) {
        return GoTimer.newTicker(ns);
    }

    /**
     * Additive (Phase 2): {@code time.Tick(d)} (no Stop handle; the
     * ticker is reclaimed at program end, gc parity).
     */
    public static ChanOps.Chan tick(long ns) {
        return GoTimer.tick(ns);
    }
}
