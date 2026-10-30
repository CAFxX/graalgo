package graalgo.runtime;

/**
 * Regression tests for review 2026-10-30-21, batch-mode state leak (P1):
 * guest {@code os.Exit} / {@code sync.throw} / {@code sync.fatal} called
 * {@code System.exit} unconditionally, killing the whole {@code --batch}
 * JVM, aborting remaining entries and losing the JSON report. Under a
 * batch trap, {@link GuestExit#exit} must throw (with the code recorded)
 * instead of exiting, so the batch loop records the entry's exit code
 * and continues.
 *
 * <p>Single-run mode (no trap) keeps the raw {@code System.exit} path
 * and is deliberately not exercised here — calling it would kill the
 * test JVM.
 */
public final class GuestExitTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testTrappedExitThrowsWithCode() {
        GuestExit.beginTrap();
        try {
            try {
                GuestExit.exit(3);
                throw new AssertionError("trapped exit should throw GuestExit");
            } catch (GuestExit e) {
                assertTrue(e.code() == 3, "trapped exit code should be 3, got " + e.code());
            }
            assertTrue(GuestExit.trapActive(), "trap should still be active inside the entry");
            assertTrue(GuestExit.trappedCode() == 3,
                    "trapped code should be recorded (also visible to the batch loop "
                            + "if the exit was raised off the entry thread), got "
                            + GuestExit.trappedCode());
        } finally {
            GuestExit.endTrap();
        }
        assertTrue(!GuestExit.trapActive(), "trap should be inactive after endTrap");
    }

    public void testTrapResetsCodeBetweenEntries() {
        GuestExit.beginTrap();
        try {
            try {
                GuestExit.exit(5);
                throw new AssertionError("trapped exit should throw GuestExit");
            } catch (GuestExit ignored) {
                // Expected; code recorded below.
            }
            assertTrue(GuestExit.trappedCode() == 5, "trapped code should be 5");
        } finally {
            GuestExit.endTrap();
        }
        GuestExit.beginTrap();
        try {
            assertTrue(GuestExit.trappedCode() == -1,
                    "a new entry's trap must start with no trapped code, got "
                            + GuestExit.trappedCode());
        } finally {
            GuestExit.endTrap();
        }
    }

    public void testTrappedExitIsAnErrorNotAnException() {
        // runOne() catches RuntimeException for execution failures: a
        // trapped exit must NOT be a RuntimeException, or the batch loop
        // would see exit 1 instead of the guest's code.
        GuestExit.beginTrap();
        try {
            try {
                GuestExit.exit(7);
                throw new AssertionError("trapped exit should throw GuestExit");
            } catch (RuntimeException e) {
                throw new AssertionError("GuestExit must not be a RuntimeException, got " + e);
            } catch (GuestExit e) {
                assertTrue(e.code() == 7, "trapped exit code should be 7");
            }
        } finally {
            GuestExit.endTrap();
        }
    }
}
