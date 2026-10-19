package graalgo.p3time;

/**
 * Monotonic-to-wall clock mapping for timer fire instants (P1-7).
 *
 * <p>The Phase-2 timer heap schedules on {@link System#nanoTime()} and
 * delivers the fire instant sampled on that clock (the "fire nanotime"
 * {@link Long} on the heap channel). Go's {@code time.Time} is wall-clock
 * unix nanos, so the translator must convert epochs. {@code
 * System#nanoTime()} has no defined epoch and {@code
 * System#currentTimeMillis()} has only millisecond granularity, so a
 * single static base pair anchors the two clocks once: a fire instant
 * converts as {@code wallBase + (fireNano - monoBase)}, preserving the
 * heap's nanosecond granularity instead of re-quantizing to millis.
 *
 * <p>The anchor skew (the two base samples are microseconds apart) and
 * nanoTime/wall drift over a timer's lifetime are both far below a
 * millisecond; the mapping is only ever applied to fire instants, never
 * to long-lived timestamps.
 */
final class FireTimeClocks {
    private static final long MONO_BASE = System.nanoTime();
    private static final long WALL_BASE_NANOS = System.currentTimeMillis() * 1_000_000L;

    private FireTimeClocks() {
    }

    /**
     * Wall-clock unix nanos corresponding to a {@link System#nanoTime()}
     * sample (e.g. a timer-heap fire instant).
     */
    static long toWallNanos(long nanoTime) {
        return WALL_BASE_NANOS + (nanoTime - MONO_BASE);
    }
}
