package graalgo.runtime;

/**
 * Unrecoverable Go fatal error (review 2026-11-05, B-P1-4: {@code GoMap}
 * concurrency guard).
 *
 * <p>An {@code Error}, not an {@code Exception}, by construction: the
 * guest-visible {@code recover()} mechanism only stops
 * {@link PanicException}, and {@link DeferChain} catches only
 * {@code PanicException}/{@code Goexit}, so a {@code GoFatalError} can
 * never be recovered by Go code — mirroring gc's {@code throw()}, which
 * bypasses deferred calls. It travels through compiled Truffle frames
 * uncaught (Truffle unwinds by deoptimizing; nothing in guest frames
 * catches it) and is routed to {@link GoFatal#die} at every guest-code
 * entry point: the scheduler spawn wrapper, the launcher {@code Main},
 * and the finalizer action. The process dies with exit code 2
 * ({@code EXIT_PANIC}).
 *
 * <p>Carries gc's exact message verbatim
 * ({@code concurrent map read and map write},
 * {@code concurrent map iteration and map write},
 * {@code concurrent map writes}).
 */
public final class GoFatalError extends Error {
    public GoFatalError(String message) {
        super(message);
    }

    public GoFatalError(String message, Throwable cause) {
        super(message, cause);
    }
}
