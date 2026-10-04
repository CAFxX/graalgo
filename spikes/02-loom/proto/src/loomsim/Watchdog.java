package loomsim;

/**
 * Models Go's sysmon-driven async preemption (forcePreemptNS = 10ms).
 * A daemon platform thread wakes every 10ms and sets preemptRequested on
 * every live goroutine — the equivalent of sysmon's retake setting
 * gp.preempt. Each goroutine honors the request at most once (it clears its
 * own flag when it yields at the next back-edge safepoint), bounding the
 * unfairness window of a compute-bound goroutine to ~10ms like gc.
 */
public final class Watchdog {
    private static final long PERIOD_NS = Long.getLong("loomsim.watchdogPeriodNs", 10_000_000L); // 10ms like forcePreemptNS

    private static volatile boolean running = false;

    /** Diagnostics: watchdog pulses issued. */
    public static final java.util.concurrent.atomic.AtomicLong pulses = new java.util.concurrent.atomic.AtomicLong();

    public static synchronized void start() {
        if (running) return;
        running = true;
        Thread.ofPlatform().daemon(true).name("loom-watchdog").start(() -> {
            while (running) {
                sleepNanos(PERIOD_NS);
                pulses.incrementAndGet();
                for (Goroutine g : Goroutine.all()) {
                    g.preemptRequested = true;
                }
            }
        });
    }

    public static synchronized void stop() {
        running = false;
    }

    private static void sleepNanos(long ns) {
        try {
            Thread.sleep(ns / 1_000_000L, (int) (ns % 1_000_000L));
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }

    private Watchdog() {}
}
