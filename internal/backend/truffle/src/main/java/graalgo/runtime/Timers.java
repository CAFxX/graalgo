package graalgo.runtime;

/**
 * Compile-time timer intrinsic surface (WS G, GG-0020; bridge B-8,
 * inventoried in {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This class exists so the truffle module compiles against the timer
 * surface despite the truffle&#8596;runtime compile cycle (the runtime module
 * compiles the joint round first; the truffle module then compiles this
 * same-FQN class against it). The real timer/sleep implementation lives in
 * the runtime module ({@code graalgo.runtime.Timers} in
 * {@code internal/backend/runtime}, backed by the scheduler timer heap).
 * At launch, {@code internal/backend/graalgo-run} puts
 * {@code runtime/out/classes} <em>before</em> {@code truffle/out/classes} on
 * the classpath, so the real implementation shadows this class — this file
 * is only ever loaded if the launcher classpath order is violated (a launch
 * bug).
 *
 * <p>{@link #nanotime} is REAL (backed by {@link System#nanoTime}, which is
 * the documented source for {@code runtime.nanotime} on linux/amd64).
 * Everything else is a stub: {@link #sleep}, {@link #startTimer}, and
 * {@link #stopTimer} throw {@link UnsupportedOperationException} because
 * they need the scheduler's park/unpark and timer heap; they must never be
 * called when the runtime module's class is correctly shadowed in.
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
