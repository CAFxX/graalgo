package graalgo.sched;

/**
 * Thrown by {@link SchedulerCore#awaitMainExit} and
 * {@link SchedulerCore#awaitQuiescence} when the deadlock watchdog fires:
 * every live goroutine is parked, no wakeup is possible (empty lanes and
 * inbox, nobody in blocking I/O, no pending timers, nobody parked in an
 * external wait such as netpoll), and that quiescent state persisted for
 * the grace period.
 *
 * <p>Unchecked so existing {@code awaitQuiescence} callers compile
 * unchanged: a deadlocking test now fails fast with this instead of
 * hanging forever. Launchers translate it to gc's
 * {@code fatal error: all goroutines are asleep - deadlock!} (exit 2).
 */
public final class DeadlockException extends RuntimeException {
    public DeadlockException() {
        super("all goroutines are asleep - deadlock");
    }
}
