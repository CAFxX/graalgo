package graalgo.p3time;

import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;

import graalgo.runtime.GoTimer;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * Regression tests for {@link P3TimeRuntime#awaitBackgroundDrain}.
 *
 * <p>L29 (P1-26): the drain must not return after a single
 * check-then-shutdown pass. A translator registered after the
 * {@code live <= bg} check — e.g. a ticker created by an AfterFunc
 * goroutine spawned in the check→shutdown window, or any timer creation
 * racing program end — must be shut down by a later pass. The old
 * single-pass code returned with such a translator still registered; a
 * stranded ticker translator never exits on its own, so the launcher's
 * {@code awaitQuiescence} hung forever.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class BackgroundDrainTest {

    private static final GoStructDescriptor TIME_DESC = GoStructDescriptor.layout(
            "time.Time", List.of("unixnano", "isSet"), List.of(GoKind.INT64, GoKind.BOOL));

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertFalse(boolean c, String msg) {
        assertTrue(!c, msg);
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /**
     * A translator registered after the drain's first shutdown pass must
     * be shut down by a later pass, not stranded.
     *
     * <p>Deterministic construction (no racer timing assumptions): an
     * anchor batch of tickers is registered up front. The spawner thread
     * spins until the registry starts draining ({@code backgroundLive}
     * drops below the anchor count — the drain's first pass has taken
     * effect and the anchor translators are exiting, a millisecond-scale
     * process), then registers one late ticker. The spawner reacts in
     * microseconds, so the late ticker is registered while the registry
     * is still far from empty: the drain cannot have observed quiescence
     * yet, on either the old or the new code.
     *
     * <p>On the old single-pass code the drain has already returned by
     * the time the late ticker registers (it returns right after its one
     * pass); the late ticker is never disarmed and the
     * {@code backgroundLive() == 0} assertion below fails. On the fixed
     * code the re-verify loop disarms the late ticker on a later pass and
     * the drain converges.
     */
    public void testDrainShutsDownLateRegisteredTranslators() throws Exception {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore prev = SchedulerCore.install(sched);
        final int anchors = 500;
        for (int i = 0; i < anchors; i++) {
            new GoTickerT(GoTimer.newTicker(50_000_000L), TIME_DESC);
        }
        AtomicBoolean spawnerDone = new AtomicBoolean(false);
        Thread spawner = new Thread(() -> {
            // Wait for the drain's first pass to take effect, then
            // register the late ticker. Spin deadline: if the drain
            // never starts (broken test setup), give up loudly below.
            long start = System.nanoTime();
            while (P3TimeRuntime.backgroundLive() >= anchors) {
                if (System.nanoTime() - start > 10_000_000_000L) {
                    return;
                }
                Thread.yield();
            }
            new GoTickerT(GoTimer.newTicker(50_000_000L), TIME_DESC);
            // Wait until the late ticker is disarmed. On the old
            // single-pass code it is stranded, so bound the wait: the
            // timeout below is the RED signal, not a hang.
            start = System.nanoTime();
            while (P3TimeRuntime.backgroundLive() != 0) {
                if (System.nanoTime() - start > 5_000_000_000L) {
                    break;
                }
                Thread.yield();
            }
            spawnerDone.set(true);
        }, "test-drain-spawner");
        Thread drainThread = new Thread(() -> {
            try {
                P3TimeRuntime.awaitBackgroundDrain(sched);
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                throw new RuntimeException(ie);
            }
        }, "test-drain");
        try {
            spawner.start();
            drainThread.start();
            spawner.join(30_000);
            assertTrue(spawnerDone.get(), "spawner finished (late ticker disarmed, or RED timeout)");
            drainThread.join(30_000);
            assertFalse(drainThread.isAlive(), "drain returned after the registry emptied");
            assertEq(0, P3TimeRuntime.backgroundLive(),
                    "no translator stranded by the drain");
            sched.awaitQuiescence();
        } finally {
            // Safety net: never let a failure here hang the suite or leak
            // live goroutines into the next test.
            P3TimeRuntime.shutdownBackground();
            sched.awaitQuiescence();
            SchedulerCore.install(prev);
        }
    }
}
