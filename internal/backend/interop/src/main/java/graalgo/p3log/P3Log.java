package graalgo.p3log;

import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Shared diagnostic logging for the interop module.
 *
 * <p>Compile/startup diagnostics (method-table construction, reflect type
 * registration, and similar launch-time work) log warnings through here
 * instead of writing to the standard error stream directly: ad-hoc
 * stderr printing is rejected by the interop {@code check} gate
 * (debug-leftover grep), and a single utility keeps the diagnostic
 * format and destination in one place.
 *
 * <p>Backed by {@link java.util.logging} (JDK standard — no extra
 * dependencies, which the plain-{@code javac} module builds require).
 * The default JUL configuration routes {@code WARNING} to stderr, so
 * warnings remain visible exactly where the old {@code System.err}
 * messages were.
 */
public final class P3Log {
    private P3Log() {}

    /**
     * Logs a diagnostic warning, attributed to the calling class.
     *
     * @param source  the class issuing the warning (used as the logger name)
     * @param message the warning text
     */
    public static void warn(Class<?> source, String message) {
        Logger logger = Logger.getLogger(source.getName());
        if (logger.isLoggable(Level.WARNING)) {
            logger.warning(message);
        }
    }
}
