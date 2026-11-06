package graalgo.runtime;

/**
 * Review 2026-11-05, B-P1-4: {@link GoFatalError} / {@link GoFatal} pins.
 *
 * <p>verifies: review 2026-11-05 B-P1-4 (a Go fatal is unrecoverable and
 * prints gc's {@code fatal error: <msg>} header)
 */
public final class GoFatalTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    private static void assertEq(String want, String got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": want [" + want + "] got [" + got + "]");
        }
    }

    public void testHeaderFormat() {
        assertEq("fatal error: concurrent map writes",
                GoFatal.header(new GoFatalError("concurrent map writes")),
                "header for concurrent map writes");
        assertEq("fatal error: concurrent map read and map write",
                GoFatal.header(new GoFatalError("concurrent map read and map write")),
                "header for concurrent map read and map write");
        assertEq("fatal error: concurrent map iteration and map write",
                GoFatal.header(new GoFatalError("concurrent map iteration and map write")),
                "header for concurrent map iteration and map write");
    }

    /**
     * Pins the unrecoverability at the type level: a {@code GoFatalError}
     * is an {@code Error}, never an {@code Exception}. If a future
     * refactor re-parents it under {@code RuntimeException}, Go
     * {@code recover()} would start swallowing fatals — this test fails
     * first.
     */
    public void testIsErrorNotException() {
        GoFatalError f = new GoFatalError("concurrent map writes");
        assertTrue(f instanceof Error, "GoFatalError must be an Error");
        // NOTE: `f instanceof Exception` does not compile — GoFatalError
        // is final, so javac rejects the check statically. Pin the same
        // property reflectively instead.
        assertTrue(!Exception.class.isAssignableFrom(GoFatalError.class),
                "GoFatalError must not be an Exception");
        assertTrue(!RuntimeException.class.isAssignableFrom(GoFatalError.class),
                "GoFatalError must not be a RuntimeException");
    }

    /**
     * A {@code PanicException} catch — the shape of every {@code recover()}
     * and {@code DeferChain} handler — cannot stop a fatal.
     */
    public void testNotCaughtByPanicCatch() {
        try {
            throw new GoFatalError("concurrent map writes");
        } catch (PanicException p) {
            throw new AssertionError("GoFatalError must not be caught as PanicException");
        } catch (GoFatalError f) {
            assertEq("concurrent map writes", f.getMessage(), "fatal message survives");
        }
    }
}
