package graalgo.jfr;

import jdk.jfr.Category;
import jdk.jfr.Description;
import jdk.jfr.Enabled;
import jdk.jfr.Event;
import jdk.jfr.Label;
import jdk.jfr.Name;

/**
 * A channel operation blocked the goroutine (P2-M6, WS F; GG-0008 JFR
 * slice; SPEC §3.10 {@code graalgo.ChannelBlock}).
 *
 * <p>Emitted at block time (before parking), so a goroutine that never
 * resumes (deadlock) still leaves a trace. Non-blocking fast-path
 * send/recv/select emit nothing — instrumenting them would drown the
 * recording at channel-heavy rates, and a non-blocking op is not
 * scheduler-visible. For select, {@code channelId} is -1 (multiple
 * channels; the op field says SELECT).
 */
@Name("graalgo.ChannelBlock")
@Label("Go Channel Block")
@Category("graalgo")
@Enabled(false)
@Description("A goroutine blocked on a channel send, receive, or select.")
public final class ChannelBlockEvent extends Event {
    private static final ChannelBlockEvent PROTOTYPE = new ChannelBlockEvent();

    @Label("Goroutine ID")
    @Description("Stable scheduler goroutine id (GoroutineHandle.id).")
    private long goroutineId;

    @Label("Channel ID")
    @Description("Channel identity (-1 for multi-channel select).")
    private long channelId;

    @Label("Operation")
    @Description("SEND, RECV, or SELECT.")
    private String operation;

    /** Enabled check (single-digit ns; safe to call on the park path). */
    public static boolean enabled() {
        return PROTOTYPE.isEnabled();
    }

    public static void emit(long goroutineId, long channelId, ChannelOp operation) {
        ChannelBlockEvent e = new ChannelBlockEvent();
        e.goroutineId = goroutineId;
        e.channelId = channelId;
        e.operation = operation.name();
        e.commit();
    }
}
