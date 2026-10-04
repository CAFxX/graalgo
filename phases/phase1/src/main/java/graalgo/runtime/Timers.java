package graalgo.runtime;

/**
 * Frozen timer intrinsic surface (WS G, GG-0020).
 *
 * <p>{@link #nanotime} is REAL (backed by {@link System#nanoTime}, which is
 * the documented source for {@code runtime.nanotime} on linux/amd64).
 * Everything else is STUB — timers need the Phase-2 scheduler/netpoller.
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
     * STUB — sleep for {@code ns} nanoseconds. Needs Phase-2 scheduler
     * park/unpark; throws until then.
     */
    public static void sleep(long ns) {
        throw new UnsupportedOperationException(
                "Phase 2: timer sleep needs the scheduler (GG-0020)");
    }

    /**
     * STUB — arm a timer. Needs the Phase-2 timer heap; throws until then.
     */
    public static void startTimer(long whenNanos, Runnable callback) {
        throw new UnsupportedOperationException(
                "Phase 2: timers need the scheduler timer heap (GG-0020)");
    }

    /**
     * STUB — disarm a timer. Needs the Phase-2 timer heap; throws until
     * then.
     */
    public static void stopTimer(long timerId) {
        throw new UnsupportedOperationException(
                "Phase 2: timers need the scheduler timer heap (GG-0020)");
    }
}
