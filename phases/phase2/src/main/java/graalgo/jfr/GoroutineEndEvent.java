package graalgo.jfr;

import jdk.jfr.Category;
import jdk.jfr.Description;
import jdk.jfr.Event;
import jdk.jfr.Label;
import jdk.jfr.Name;

/**
 * A scheduler goroutine finished (P2-M6, WS F; GG-0008 JFR slice).
 *
 * <p>Emitted once per goroutine in the vthread's finally block, before
 * the EXIT token — so every {@code graalgo.GoroutineStart} has a
 * matching end unless the VM dies first.
 */
@Name("graalgo.GoroutineEnd")
@Label("Go Goroutine End")
@Category("graalgo")
@Description("A graalgo scheduler goroutine finished executing.")
public final class GoroutineEndEvent extends Event {
    private static final GoroutineEndEvent PROTOTYPE = new GoroutineEndEvent();

    @Label("Goroutine ID")
    @Description("Stable scheduler goroutine id (GoroutineHandle.id).")
    private long goroutineId;

    @Label("Lifetime")
    @Description("Nanos from goroutine start to end.")
    private long lifetimeNanos;

    /** Cheap enabled check: a single field read, no allocation. */
    public static boolean enabled() {
        return PROTOTYPE.isEnabled();
    }

    public static void emit(long goroutineId, long lifetimeNanos) {
        GoroutineEndEvent e = new GoroutineEndEvent();
        e.goroutineId = goroutineId;
        e.lifetimeNanos = lifetimeNanos;
        e.commit();
    }
}
