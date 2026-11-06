package graalgo.runtime;

/**
 * Shared process-fatal path for {@link GoFatalError} (review 2026-11-05,
 * B-P1-4).
 *
 * <p>Every guest-code entry point that can observe a {@code GoFatalError}
 * routes here instead of swallowing it: the scheduler spawn wrapper
 * ({@code graalgo.sched.SchedulerCore}), the launcher
 * ({@code graalgo.launcher.Main}), and the finalizer action
 * ({@code GcInterop.FinalizerAction}). A single helper keeps the fatal
 * discipline uniform — no site may invent its own variant (in particular,
 * none may print-and-continue: a fatal is process-fatal, exit 2).
 *
 * <p>Not routed here: the timer thread ({@code TimerHeap}) — verified
 * from disk that its only callback is the scheduler-internal deadline
 * wakeup ({@code GoNetpoll.armDeadline} calls
 * {@code SchedulerCore.timerHeap().addCallback} directly, and no
 * {@code Timers.startTimer}/{@code startTimerId} caller exists in main
 * code at all), so no Go code runs on it and no fatal can arise there.
 * If a guest closure is ever registered on the timer thread, its
 * {@code catch (Throwable)} in {@code TimerHeap.fireNonChan} would
 * swallow the fatal and must route it here instead.
 *
 * <p>{@code System.exit(2)} vs {@code Runtime.halt(2)}: {@code exit}
 * runs JVM shutdown hooks (stderr flush, JFR dump) while no graalgo hook
 * depends on clean scheduler state; {@code halt} would skip hooks and
 * could truncate output. Documented assumption: graalgo is a CLI
 * toolchain, not an embeddable library — an embedder would not tolerate
 * {@code System.exit}. Note the spawn wrapper's {@code finally} is
 * skipped by {@code exit} (no EXIT token, no live-count decrement) —
 * harmless at process death.
 */
public final class GoFatal {
    private GoFatal() {
    }

    /**
     * The {@code fatal error: <msg>} header line, extracted so the format
     * is unit-testable without forking a JVM.
     */
    public static String header(GoFatalError f) {
        return "fatal error: " + f.getMessage();
    }

    /**
     * Prints the fatal header, a blank line, and the faulting goroutine's
     * Go stack (when the fatal is observed on a goroutine thread;
     * {@code goroutineId < 0} means a runtime thread with no Go stack),
     * flushes stderr, then dies with exit code 2. Never returns.
     *
     * @param goroutineId the faulting goroutine's id (1 = main), or a
     *        negative value when the fatal is raised on a runtime thread
     *        (finalizer thread)
     */
    public static void die(long goroutineId, GoFatalError f) {
        System.err.println(header(f));
        System.err.println();
        if (goroutineId >= 0) {
            GoStackFrame top = null;
            try {
                GoroutineState s = GoroutineState.current();
                if (s != null) {
                    top = s.goStack();
                }
            } catch (Throwable ignored) {
                // Best effort: a missing/broken goroutine state must not
                // mask the fatal itself.
            }
            System.err.print(Intrinsics.renderGoStack(goroutineId, top, null));
        } else {
            System.err.println("(fatal on runtime thread: no goroutine stack)");
        }
        System.err.flush();
        System.exit(2);
        throw new AssertionError("GoFatal.die: System.exit returned");
    }
}
