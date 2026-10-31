package graalgo.p3log;

import java.util.ArrayList;
import java.util.List;
import java.util.logging.Handler;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;

/**
 * Unit tests for {@link P3Log} (R-03 remediation, 2026-10-31).
 *
 * <p>{@code P3Log} is a thin wrapper over {@code java.util.logging},
 * kept as the single diagnostic channel because the interop
 * {@code check} gate rejects ad-hoc stderr printing. The tests pin
 * the only observable contract: a warning is emitted at WARNING level
 * on the logger named for the calling class, with the message intact.
 * A wrapper that dropped the message, logged at the wrong level, or
 * attributed it to the wrong logger would break launch-time
 * diagnostics silently, so the test captures records directly.
 */
public final class P3LogTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    public void testWarnEmitsWarningOnCallerLogger() {
        Logger logger = Logger.getLogger(P3LogTest.class.getName());
        List<LogRecord> seen = new ArrayList<>();
        Handler capture = new Handler() {
            @Override
            public void publish(LogRecord record) {
                seen.add(record);
            }

            @Override
            public void flush() {
            }

            @Override
            public void close() {
            }
        };
        Level oldLevel = logger.getLevel();
        boolean oldUseParent = logger.getUseParentHandlers();
        logger.setLevel(Level.ALL);
        logger.setUseParentHandlers(false);
        logger.addHandler(capture);
        try {
            P3Log.warn(P3LogTest.class, "test diagnostic message");
            assertTrue(seen.size() == 1, "exactly one record must be emitted, got " + seen.size());
            assertTrue(seen.get(0).getLevel() == Level.WARNING,
                    "record must be WARNING, got " + seen.get(0).getLevel());
            assertTrue("test diagnostic message".equals(seen.get(0).getMessage()),
                    "record message must be intact, got " + seen.get(0).getMessage());
            assertTrue(P3LogTest.class.getName().equals(seen.get(0).getLoggerName()),
                    "record must be attributed to the caller class logger");
        } finally {
            logger.removeHandler(capture);
            logger.setLevel(oldLevel);
            logger.setUseParentHandlers(oldUseParent);
        }
    }

    public void testWarnDoesNotThrowForAnyCaller() {
        // A diagnostic path must never break the launch it diagnoses.
        P3Log.warn(String.class, "some other caller");
        P3Log.warn(P3LogTest.class, "");
    }
}
