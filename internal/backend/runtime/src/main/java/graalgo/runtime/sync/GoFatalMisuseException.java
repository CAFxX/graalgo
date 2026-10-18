package graalgo.runtime.sync;

/**
 * A {@code sync} primitive misuse that is a <em>fatal error</em> in gc
 * (unrecoverable, {@code fatal error: ...} on stderr, exit 2) rather than
 * a recoverable Go panic.
 *
 * <p>gc 1.27.1 fatal-errors on mutex misuse
 * ({@code sync: unlock of unlocked mutex},
 * {@code sync: Unlock of unlocked RWMutex},
 * {@code sync: RUnlock of unlocked RWMutex} — {@code runtime.throw},
 * unrecoverable), while a negative {@code sync.WaitGroup} counter is a
 * recoverable panic. The primitives signal both with
 * {@link IllegalStateException} carrying gc's message text; this subtype
 * marks the fatal-error cases so the binding layer
 * ({@code graalgo.p3sync.nodes.SyncOpNode}) can route them to the fatal
 * path instead of {@code Panic.throwPanic}.
 *
 * <p>Extends {@link IllegalStateException} (not a new root type) so every
 * existing Java-level caller and test that catches the misuse signal
 * keeps working unchanged.
 */
public final class GoFatalMisuseException extends IllegalStateException {
    public GoFatalMisuseException(String message) {
        super(message);
    }
}
