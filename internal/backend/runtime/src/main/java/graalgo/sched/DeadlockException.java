package graalgo.sched;

/**
 * Thrown by {@link SchedulerCore#awaitQuiescence()} when the drainer's
 * deadlock detector fires (RV-P1-19): every live goroutine is parked,
 * nothing is in flight, no timer or I/O can wake anyone, and no further
 * spawn is in progress.
 *
 * <p>Unchecked so existing {@code awaitQuiescence} callers (which never
 * deadlock) need no signature change. Carries the gc-style fatal report
 * (first line byte-identical to gc's
 * {@code fatal error: all goroutines are asleep - deadlock!}, followed by
 * a per-goroutine dump). The launcher prints it to stderr and exits 2
 * (gc parity); tests assert on it directly.
 */
public final class DeadlockException extends RuntimeException {
    private final String report;

    public DeadlockException(String report) {
        super("deadlock detected: all goroutines are asleep");
        this.report = report;
    }

    /** The gc-style fatal report, ready to print to stderr. */
    public String getReport() {
        return report;
    }
}
