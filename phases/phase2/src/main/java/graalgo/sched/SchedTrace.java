package graalgo.sched;

import java.util.concurrent.atomic.AtomicBoolean;

/**
 * P2-M5 (WS E): GODEBUG=schedtrace emulation.
 *
 * <p>When {@code GODEBUG=schedtrace=N} (N &gt; 0), a daemon thread prints a
 * scheduler-state line to stderr every N milliseconds until the JVM exits:
 *
 * <pre>
 * SCHED 1008ms: goroutines=12 cap=8 carriers=8
 * </pre>
 *
 * <p>With {@code scheddetail=1}, a second line adds quantum and
 * max-in-flight detail:
 *
 * <pre>
 * SCHED-DETAIL 1008ms: quantum=10ms maxinflight=12
 * </pre>
 *
 * <p>The format is graalgo-specific and documented in
 * {@code phases/phase2/GODEBUG.md}: gc's P/M fields have no counterpart in
 * the park/queue/drainer design, so byte-identity with gc's
 * {@code SCHED ...ms: gomaxprocs=...} lines is not attempted. The emulated
 * observable behavior is "a periodic scheduler-state line on stderr".
 *
 * <p>The daemon polls {@link SchedulerCore#current()}; ticks before the
 * scheduler is installed (or after it is uninstalled) are skipped, so
 * starting early at launcher startup is safe. Start is idempotent: only
 * the first call takes effect, so repeated {@code GoDebug.apply()} calls
 * (tests) cannot stack daemons.
 */
public final class SchedTrace {
    private SchedTrace() {}

    private static final AtomicBoolean started = new AtomicBoolean(false);
    private static volatile Thread daemon;

    /**
     * Starts the periodic scheduler trace. Idempotent.
     *
     * @param millis interval between lines, must be &gt; 0
     * @param detail also print SCHED-DETAIL lines (scheddetail=1)
     */
    public static synchronized void start(int millis, boolean detail) {
        if (millis <= 0) {
            throw new IllegalArgumentException("schedtrace millis must be > 0");
        }
        if (!started.compareAndSet(false, true)) {
            return;
        }
        final long t0 = System.currentTimeMillis();
        Thread t = new Thread(() -> {
            for (;;) {
                try {
                    Thread.sleep(millis);
                } catch (InterruptedException e) {
                    return;
                }
                SchedulerCore s;
                try {
                    s = SchedulerCore.current();
                } catch (IllegalStateException e) {
                    // No scheduler installed (before SchedMain installs one,
                    // or after uninstall): skip the tick.
                    continue;
                }
                long elapsed = System.currentTimeMillis() - t0;
                System.err.println("SCHED " + elapsed + "ms: goroutines="
                        + s.runnableCount()
                        + " cap=" + s.cap() + " carriers=" + s.carrierParallelism());
                if (detail) {
                    System.err.println("SCHED-DETAIL " + elapsed + "ms: quantum="
                            + (SchedulerCore.DEFAULT_QUANTUM_NANOS / 1_000_000)
                            + "ms maxinflight=" + s.maxInflightObserved());
                }
            }
        }, "graalgo-schedtrace");
        t.setDaemon(true);
        daemon = t;
        t.start();
    }

    /** Stops the daemon (tests). No-op if never started. */
    public static synchronized void stop() {
        Thread t = daemon;
        daemon = null;
        started.set(false);
        if (t != null) {
            t.interrupt();
        }
    }

    /** Whether the daemon is currently running (tests). */
    public static boolean isRunning() {
        Thread t = daemon;
        return t != null && t.isAlive();
    }
}
