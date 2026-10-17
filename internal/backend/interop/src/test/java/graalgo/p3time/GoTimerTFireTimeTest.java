package graalgo.p3time;

import java.lang.reflect.Field;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.runtime.GoChan;
import graalgo.runtime.GoTimer;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Fire-instant tests for {@link GoTimerT} (RV-P1-22): the {@code time.Time}
 * delivered on {@code t.C} / {@code time.After} / {@code Ticker.C} must be
 * the heap's fire instant (mapped nanoTime→wall), never the translator's
 * delivery-time wall read.
 *
 * <p>{@link #testMappingHonorsFireInstantNotDelivery} is the falsifier:
 * synthesizing a 2s-old fire instant, the old delivery-time stamp is ~2s
 * off while the mapping lands within 100ms — this assertion fails on the
 * pre-fix implementation by construction (the pre-fix delivery path
 * stamped {@code System.currentTimeMillis() * 1e6} at delivery).
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class GoTimerTFireTimeTest {

    private static final GoStructDescriptor TIME_DESC = GoStructDescriptor.layout(
            "time.Time", List.of("unixnano", "isSet"), List.of(GoKind.INT64, GoKind.BOOL));

    // ---------- scaffolding (mirrors TimeChanLenCapTest) ----------

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    /** Runs the body, polling the condition until true or the timeout (ms) elapses. */
    private static void waitUntil(java.util.function.BooleanSupplier cond, long timeoutMs,
                                  String what) throws Exception {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (!cond.getAsBoolean()) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError("timed out waiting for: " + what);
            }
            Thread.sleep(5);
        }
    }

    /** Runs {@code body} on a scheduler goroutine, rethrowing failures on the test thread. */
    private static void inGoroutine(SchedulerCore sched, ThrowingRunnable body) throws Exception {
        AtomicReference<Throwable> err = new AtomicReference<>();
        CountDownLatch done = new CountDownLatch(1);
        sched.spawn(() -> {
            try {
                body.run();
            } catch (Throwable t) {
                err.set(t);
            } finally {
                done.countDown();
            }
        });
        assertTrue(done.await(30, TimeUnit.SECONDS), "test goroutine timed out");
        if (err.get() != null) {
            throw new AssertionError("test goroutine failed", err.get());
        }
    }

    private static void withScheduler(ThrowingRunnable body) throws Exception {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            // Safety net: no parked translator may survive into quiescence.
            P3TimeRuntime.shutdownBackground();
            sched.awaitQuiescence();
            if (P3TimeRuntime.backgroundLive() != 0) {
                throw new AssertionError(
                        "background translators at end: " + P3TimeRuntime.backgroundLive());
            }
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- falsifying probe (fails on the pre-fix implementation) ----------

    /**
     * RV-P1-22 falsifier: a fire instant 2s in the past must map to ~2s-ago
     * wall time. The pre-fix delivery path stamped
     * {@code System.currentTimeMillis() * 1e6} at delivery, which is ~2s
     * off here — so this assertion is false for the old behavior and true
     * for the mapping.
     */
    public void testMappingHonorsFireInstantNotDelivery() {
        long offset = GoTimerT.captureNanoToWallOffset();
        // Synthesize a fire instant 2s in the past, nanoTime domain.
        long fireNano = System.nanoTime() - 2_000_000_000L;
        long expectedWallNanos = System.currentTimeMillis() * 1_000_000L - 2_000_000_000L;
        long mapped = GoTimerT.fireToUnixNanos(fireNano, offset);
        long skew = Math.abs(mapped - expectedWallNanos);
        assertTrue(skew < 100_000_000L,
                "mapped fire instant must be ~2s in the past, skew=" + skew + "ns "
                        + "(mapped=" + mapped + ", expected~" + expectedWallNanos + ")");
    }

    /**
     * Negative control for the pairing: a fresh pairing of "now" must land
     * within 50ms of the wall clock. A ms↔ns unit slip is ~55 years off;
     * a sign error is ~110 years off — this catches both.
     */
    public void testOffsetPairingSane() {
        long now = System.nanoTime();
        long mapped = GoTimerT.fireToUnixNanos(now, GoTimerT.captureNanoToWallOffset());
        long wallNanos = System.currentTimeMillis() * 1_000_000L;
        long skew = Math.abs(mapped - wallNanos);
        assertTrue(skew < 50_000_000L, "offset pairing must be sane, skew=" + skew + "ns");
    }

    /**
     * Pins the {@code reset()} refresh: with a stale sentinel planted in
     * the offset field, {@code reset()} must re-pair the clocks. Fails if
     * the refresh is missing (sentinel persists) and cannot pass on
     * pre-fix code (no offset field at all).
     */
    public void testResetRefreshesOffset() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            GoTimerT t = new GoTimerT(GoTimer.newTimer(5_000_000_000L), false, null, TIME_DESC);
            Field f = GoTimerT.class.getDeclaredField("nanoToWallOffset");
            f.setAccessible(true);
            // Stale sentinel: provably not a fresh clock pairing.
            f.setLong(t, 0x5A5A5A5A5A5A5A5AL);
            t.reset(5_000_000_000L);
            long after = f.getLong(t);
            long fresh = GoTimerT.captureNanoToWallOffset();
            long skew = Math.abs(after - fresh);
            assertTrue(skew < 50_000_000L,
                    "reset() must re-capture the offset, skew=" + skew + "ns");
            assertTrue(t.stop(), "stop -> true");
        }));
    }

    // ---------- wiring / regression probes (pass under old and new code) ----------

    /**
     * The delivered one-shot {@code Time} is bounded: never before the
     * deadline (catches sign/unit errors in the wiring) and never in the
     * future. Passes under both old and new code by design.
     */
    public void testOneShotFireValueBounded() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            long beforeNanos = System.currentTimeMillis() * 1_000_000L;
            GoTimerT t = new GoTimerT(GoTimer.newTimer(100_000_000L), false, null, TIME_DESC);
            GoChan.RecvResult r = t.userChannel().recv();
            long afterNanos = System.currentTimeMillis() * 1_000_000L;
            assertTrue(r.value() instanceof GoStruct,
                    "fire value is a Time struct, got " + r.value());
            long nanos = ((GoStruct) r.value()).getLong(0);
            assertTrue(nanos >= beforeNanos + 100_000_000L - 60_000_000L,
                    "fire Time must not be before the deadline, got " + nanos);
            assertTrue(nanos <= afterNanos + 60_000_000L,
                    "fire Time must not be in the future, got " + nanos);
        }));
    }

    /**
     * Each ticker tick carries its own heap fire instant through the heap
     * re-arms: strictly non-decreasing, each inside its period's window.
     */
    public void testTickerTicksNonDecreasing() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            long creationWallNanos = System.currentTimeMillis() * 1_000_000L;
            GoTickerT t = new GoTickerT(GoTimer.newTicker(50_000_000L), TIME_DESC);
            TimeChan ch = t.userChannel();
            long prev = -1;
            for (int i = 0; i < 3; i++) {
                GoChan.RecvResult r = ch.recv();
                assertTrue(r.value() instanceof GoStruct,
                        "tick " + i + " is a Time struct");
                long nanos = ((GoStruct) r.value()).getLong(0);
                assertTrue(nanos >= prev,
                        "tick " + i + " must be non-decreasing: " + nanos + " < " + prev);
                long nowNanos = System.currentTimeMillis() * 1_000_000L;
                assertTrue(nanos >= creationWallNanos + (i + 1) * 50_000_000L - 60_000_000L,
                        "tick " + i + " must not be before its period, got " + nanos);
                assertTrue(nanos <= nowNanos + 60_000_000L,
                        "tick " + i + " must not be in the future, got " + nanos);
                prev = nanos;
            }
            assertTrue(t.stop(), "ticker stop -> true");
            waitUntil(() -> P3TimeRuntime.backgroundLive() == 0, 5_000,
                    "ticker translator exited after stop");
        }));
    }
}
