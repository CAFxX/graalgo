package graalgo.jfr;

import graalgo.sched.GoroutineHandle;

/**
 * Central JFR emission point for scheduler observability (P2-M6, WS F;
 * GG-0008 JFR slice).
 *
 * <p>Overhead contract: when no recording has our events enabled, every
 * method below costs at most two field reads and a branch — no
 * allocation, no {@code System.nanoTime()}. The {@code isEnabled()}
 * checks read the JFR event-state field the JVM flips when a recording
 * starts/stops, so the fast path stays fast in production.
 *
 * <p>Rate contract: {@code graalgo.GoroutinePark} is committed only for
 * parks lasting at least {@link #PARK_THRESHOLD_NANOS} (quantum parks are
 * microseconds; real blocks are milliseconds). {@code graalgo.ChannelBlock}
 * fires only when an operation actually blocks; non-blocking fast-path
 * channel ops emit nothing.
 */
public final class JfrEvents {
    private JfrEvents() {}

    /**
     * Parks shorter than this are not committed as
     * {@code graalgo.GoroutinePark} events. Default 100&nbsp;µs:
     * quantum-park round-trips measure single-digit microseconds, so the
     * scheduler hot loop stays silent, while channel/timer/select blocks
     * (milliseconds) are recorded. Override with
     * {@code -Dgraalgo.jfr.parkThresholdNanos=<nanos>}.
     */
    public static final long PARK_THRESHOLD_NANOS =
            Long.getLong("graalgo.jfr.parkThresholdNanos", 100_000L);

    /**
     * Force-loads the event classes so {@code Recording.enable(name)}
     * resolves them. Referencing the class literals triggers loading,
     * which registers the events with JFR.
     */
    public static void ensureRegistered() {
        Class<?>[] events = {
            GoroutineStartEvent.class,
            GoroutineEndEvent.class,
            GoroutineParkEvent.class,
            ChannelBlockEvent.class,
        };
        if (events.length != 4) {
            throw new AssertionError("event registration list changed");
        }
    }

    /** True if any graalgo event is enabled in any active recording. */
    public static boolean anyEnabled() {
        return GoroutineStartEvent.enabled()
                || GoroutineEndEvent.enabled()
                || GoroutineParkEvent.enabled()
                || ChannelBlockEvent.enabled();
    }

    /** Goroutine started executing (call after first admission). */
    public static void goroutineStart(GoroutineHandle h) {
        if (GoroutineStartEvent.enabled()) {
            Thread t = h.thread;
            GoroutineStartEvent.emit(h.id, t == null ? "" : t.getName());
        }
    }

    /** Goroutine finished (call in the vthread finally block). */
    public static void goroutineEnd(GoroutineHandle h, long lifetimeNanos) {
        if (GoroutineEndEvent.enabled()) {
            GoroutineEndEvent.emit(h.id, lifetimeNanos);
        }
    }

    /**
     * Records that {@code h} is parking for {@code reason}. Call before
     * the park becomes visible (same place as {@code armPark}).
     */
    public static void notePark(GoroutineHandle h, ParkReason reason) {
        h.parkReason = reason;
        h.parkStartNanos = System.nanoTime();
    }

    /**
     * Called when a park's {@code awaitAck} returns. Commits
     * {@code graalgo.GoroutinePark} iff the park lasted at least
     * {@link #PARK_THRESHOLD_NANOS}. Consumes the parked reason
     * (idempotent per park).
     */
    public static void parkResumed(GoroutineHandle h) {
        ParkReason reason = h.parkReason;
        if (reason == null) {
            return;
        }
        h.parkReason = null;
        if (!GoroutineParkEvent.enabled()) {
            return;
        }
        long durationNanos = System.nanoTime() - h.parkStartNanos;
        if (durationNanos >= PARK_THRESHOLD_NANOS) {
            GoroutineParkEvent.emit(h.id, reason, durationNanos);
        }
    }

    /** A channel operation blocked (call before parking). */
    public static void channelBlock(GoroutineHandle h, long channelId, ChannelOp op) {
        if (ChannelBlockEvent.enabled()) {
            ChannelBlockEvent.emit(h.id, channelId, op);
        }
    }

    /**
     * A blocking-I/O interval finished (call after {@code ioExit}'s
     * re-admission). The full syscall duration is reported, unlike the
     * microsecond slot-release handshake which {@link #parkResumed}
     * filters.
     */
    public static void ioBlocked(GoroutineHandle h, long ioNanos) {
        if (GoroutineParkEvent.enabled() && ioNanos >= PARK_THRESHOLD_NANOS) {
            GoroutineParkEvent.emit(h.id, ParkReason.IO, ioNanos);
        }
    }
}
