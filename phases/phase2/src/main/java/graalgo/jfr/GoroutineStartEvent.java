package graalgo.jfr;

import jdk.jfr.Category;
import jdk.jfr.Description;
import jdk.jfr.Event;
import jdk.jfr.Label;
import jdk.jfr.Name;

/**
 * A scheduler goroutine started executing (P2-M6, WS F; GG-0008 JFR slice).
 *
 * <p>Emitted once per goroutine when its virtual thread passes the first
 * admission. Correlates with the JDK's {@code jdk.VirtualThreadStart} via
 * {@code threadName} ({@code "goroutine-<id>"}); the JDK event carries no
 * scheduler id, this one carries no stack.
 */
@Name("graalgo.GoroutineStart")
@Label("Go Goroutine Start")
@Category("graalgo")
@Description("A graalgo scheduler goroutine started executing on its virtual thread.")
public final class GoroutineStartEvent extends Event {
    private static final GoroutineStartEvent PROTOTYPE = new GoroutineStartEvent();

    @Label("Goroutine ID")
    @Description("Stable scheduler goroutine id (GoroutineHandle.id).")
    private long goroutineId;

    @Label("Thread Name")
    @Description("Virtual thread name (goroutine-<id>).")
    private String threadName;

    /**
     * Cheap enabled check: a single field read, no allocation. This is
     * what keeps the scheduler fast path near-zero cost when no
     * recording is active.
     */
    public static boolean enabled() {
        return PROTOTYPE.isEnabled();
    }

    public static void emit(long goroutineId, String threadName) {
        GoroutineStartEvent e = new GoroutineStartEvent();
        e.goroutineId = goroutineId;
        e.threadName = threadName;
        e.commit();
    }
}
