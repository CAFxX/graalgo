package graalgo.sched;

import java.util.concurrent.atomic.AtomicInteger;

/**
 * The Go-observable {@code GOMAXPROCS} value for the process.
 *
 * <p>In gc, {@code runtime.GOMAXPROCS(n)} both reports and <em>changes</em>
 * the number of operating-system threads that can execute user-level Go
 * code simultaneously. graalgo honors both halves:
 * <ul>
 *   <li>The observed value (this cell) is what
 *       {@code runtime.GOMAXPROCS(0)} returns and what
 *       {@code graalgo.p3link.SyncRuntime#procPin} sizes its pin-id pool
 *       by.</li>
 *   <li>A positive {@code n} also moves the installed
 *       {@link SchedulerCore}'s effective in-flight cap
 *       ({@link SchedulerCore#setGomaxprocs}), so the change is real, not
 *       bookkeeping.</li>
 * </ul>
 *
 * <p>Lifecycle: the value starts at the host CPU count; every
 * {@link SchedulerCore} construction publishes its requested GOMAXPROCS
 * (the launcher {@code --gmp} flag flows through the constructor, so no
 * launcher can forget the sync — R-2). Each launcher run therefore
 * starts from a fresh, correct value even when many programs share one
 * JVM (the differential runner).
 *
 * <p>This lives in the runtime module (not next to the
 * {@code runtimegomaxprocs} wire node in interop) so that the
 * runtime-module launchers ({@code graalgo.sched.launcher.SchedMain})
 * can observe it without an interop dependency.
 */
public final class Gomaxprocs {
    private Gomaxprocs() {}

    /** The current GOMAXPROCS value; initialized to the host CPU count. */
    private static final AtomicInteger CURRENT =
            new AtomicInteger(Runtime.getRuntime().availableProcessors());

    /**
     * Returns the current GOMAXPROCS value — the same value Go code
     * observes through {@code runtime.GOMAXPROCS(0)}.
     */
    public static int current() {
        return CURRENT.get();
    }

    /**
     * Startup sync: publishes the launcher's requested GOMAXPROCS
     * (the {@code --gmp} flag, defaulting to the host CPU count).
     * Called by the {@link SchedulerCore} constructor, so every
     * scheduler — launcher or test — starts from the value it was
     * actually built with.
     */
    public static void setInitial(int gmp) {
        CURRENT.set(Math.max(1, gmp));
    }

    /**
     * {@code runtime.GOMAXPROCS(n)} with {@code n > 0}: publishes the new
     * observed value and pushes it to the installed scheduler's
     * effective cap (a no-op when no scheduler is installed — Phase-1
     * bookkeeping parity).
     *
     * @return the previous observed value (gc: GOMAXPROCS returns the
     *         old setting).
     */
    public static int set(int n) {
        int want = Math.max(1, n);
        int prev = CURRENT.getAndSet(want);
        SchedulerCore sc = SchedulerCore.installed();
        if (sc != null) {
            sc.setGomaxprocs(want);
        }
        return prev;
    }
}
