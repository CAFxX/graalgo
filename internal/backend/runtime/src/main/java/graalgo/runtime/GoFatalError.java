package graalgo.runtime;

/**
 * A Go fatal error (gc's {@code throw()}): unrecoverable, prints
 * {@code fatal error: <msg>} on stderr, and the process exits with
 * status 2.
 *
 * <p>Thrown for programmer misuse the language defines as fatal rather
 * than panicking — currently {@code sync.Mutex} /
 * {@code sync.RWMutex} unlock-of-unlocked ({@code sync/mutex.go} and
 * {@code sync/rwmutex.go} in go1.27.1 call {@code throw()}, not
 * {@code panic()}). Unlike {@link PanicException} it is never
 * recoverable: it propagates through deferred functions untouched
 * ({@link DeferChain} only unwraps {@code PanicException}), and the
 * scheduler records it via a dedicated fatal path (exit 2), never via
 * the panic path.
 *
 * <p>Extends {@link RuntimeException} (not {@link Error}) so a missed
 * catch degrades to the loud internal-error path instead of killing the
 * JVM thread with a raw stack.
 */
public final class GoFatalError extends RuntimeException {
    public GoFatalError(String message) {
        super(message);
    }
}
