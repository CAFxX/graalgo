package graalgo.jfr;

import jdk.jfr.Category;
import jdk.jfr.Description;
import jdk.jfr.Enabled;
import jdk.jfr.Event;
import jdk.jfr.Label;
import jdk.jfr.Name;

/**
 * A scheduler goroutine was parked and has now resumed (P2-M6, WS F;
 * GG-0008 JFR slice).
 *
 * <p>Committed on resume with the measured park duration. Rate control:
 * non-quantum parks are committed only when lasting at least
 * {@link JfrEvents#PARK_THRESHOLD_NANOS}; quantum parks (the scheduler
 * hot loop) are sampled 1-in-1024. See {@link JfrEvents#parkResumed}.
 */
@Name("graalgo.GoroutinePark")
@Label("Go Goroutine Park")
@Category("graalgo")
@Enabled(false)
@Description("A graalgo scheduler goroutine parked and resumed; long parks only (see graalgo.jfr.parkThresholdNanos).")
public final class GoroutineParkEvent extends Event {
    private static final GoroutineParkEvent PROTOTYPE = new GoroutineParkEvent();

    @Label("Goroutine ID")
    @Description("Stable scheduler goroutine id (GoroutineHandle.id).")
    private long goroutineId;

    @Label("Park Reason")
    @Description("Why the goroutine parked: CHANNEL, TIMER, SELECT, QUANTUM, IO, UNKNOWN.")
    private String reason;

    @Label("Park Duration")
    @Description("Nanos parked before resume.")
    private long durationNanos;

    /** Enabled check (single-digit ns; safe to call on the park path). */
    public static boolean enabled() {
        return PROTOTYPE.isEnabled();
    }

    /**
     * Begins an interval event (reviewer2 P2-7): for blocking I/O, begun
     * at {@code ioEnter} and committed at {@code ioExit} so the JFR
     * interval spans the syscall instead of rendering at the exit
     * instant. Call {@link #commitInterval} to fill and commit, or drop
     * the reference to discard.
     */
    public static GoroutineParkEvent beginInterval() {
        GoroutineParkEvent e = new GoroutineParkEvent();
        e.begin();
        return e;
    }

    /** Fills the fields of a begun interval and commits it. */
    public void commitInterval(long goroutineId, String reason, long durationNanos) {
        this.goroutineId = goroutineId;
        this.reason = reason;
        this.durationNanos = durationNanos;
        this.commit();
    }

    public static void emit(long goroutineId, ParkReason reason, long durationNanos) {
        GoroutineParkEvent e = new GoroutineParkEvent();
        e.goroutineId = goroutineId;
        e.reason = reason.name();
        e.durationNanos = durationNanos;
        e.commit();
    }
}
