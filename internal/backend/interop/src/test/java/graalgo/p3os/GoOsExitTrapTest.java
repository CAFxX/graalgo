package graalgo.p3os;

import graalgo.p3link.SyncRuntime;
import graalgo.runtime.GuestExit;
import graalgo.values.GoString;

/**
 * Regression tests for review 2026-10-30-21, batch-mode state leak
 * (P1): guest exits must be trapped per batch entry at every call
 * site, not just in the shared {@link GuestExit} mechanics — a future
 * call site that bypassed the trap would kill the whole batch JVM
 * again. Each test drives the real call site under a trap and requires
 * the guest's code back (the JVM must survive the call).
 *
 * <p>Single-run mode (no trap) keeps the raw {@code System.exit} path
 * and is deliberately not exercised here.
 */
public final class GoOsExitTrapTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testOsExitTrappedInBatch() {
        GuestExit.beginTrap();
        try {
            try {
                GoOs.exit(3);
                throw new AssertionError("trapped os.Exit should throw GuestExit");
            } catch (GuestExit e) {
                assertTrue(e.code() == 3, "trapped os.Exit code should be 3, got " + e.code());
            }
        } finally {
            GuestExit.endTrap();
        }
    }

    public void testSyncThrowTrappedInBatch() {
        GuestExit.beginTrap();
        try {
            try {
                SyncRuntime.syncThrow(GoString.fromUtf8("boom"));
                throw new AssertionError("trapped sync.throw should throw GuestExit");
            } catch (GuestExit e) {
                assertTrue(e.code() == 2, "trapped sync.throw code should be 2, got " + e.code());
            }
        } finally {
            GuestExit.endTrap();
        }
    }

    public void testSyncFatalTrappedInBatch() {
        GuestExit.beginTrap();
        try {
            try {
                SyncRuntime.syncFatal(GoString.fromUtf8("boom"));
                throw new AssertionError("trapped sync.fatal should throw GuestExit");
            } catch (GuestExit e) {
                assertTrue(e.code() == 2, "trapped sync.fatal code should be 2, got " + e.code());
            }
        } finally {
            GuestExit.endTrap();
        }
    }
}
