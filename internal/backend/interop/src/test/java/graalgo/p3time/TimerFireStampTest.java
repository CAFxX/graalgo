package graalgo.p3time;

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
 * R-5 red-green: {@code time.Time} values delivered on timer channels
 * must be stamped at the FIRE time, not at delivery.
 *
 * <p>The old code stamped
 * {@code System.currentTimeMillis() * 1_000_000L} at the moment the
 * translator goroutine delivered the value: millisecond resolution,
 * translator latency included, and no monotonic basis — so
 * {@code time.Since} on a {@code <-timer.C} value degraded to a
 * delivery-time wall-clock difference. The fix anchors the wall clock
 * at arm time and advances it by the monotonic delta to the fire
 * nanotime the heap provides:
 * {@code armWallNanos + (fireMono - armMonoNanos)}.
 *
 * <p>(The synthetic {@code time.Time} is
 * {@code struct{ unixnano int64; isSet bool }} — the second long slot
 * is the isSet flag, not a monotonic reading; see
 * {@link TimeDescriptors}. Monotonic accuracy here means the stamps
 * advance by true elapsed nanotime between fires.)
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimerFireStampTest {

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

    private static void assertInRange(long v, long lo, long hi, String msg) {
        if (v < lo || v > hi) {
            throw new AssertionError(msg + ": value=" + v
                    + " not in [" + lo + ", " + hi + "]");
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
            P3TimeRuntime.shutdownBackground();
            sched.awaitQuiescence();
            long live = P3TimeRuntime.backgroundLive();
            assertTrue(live == 0, "background translators at end: " + live);
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** Receives one Time value from the timer's user channel and returns its unixnano. */
    private static long recvStamp(GoTimerT t) {
        GoChan.RecvResult r = t.userChannel().recv();
        assertTrue(r.value() instanceof GoStruct,
                "fire value is a Time struct, got " + r.value());
        return ((GoStruct) r.value()).getLong(0);
    }

    // ---------- tests ----------

    /**
     * A one-shot timer's delivered Time is within tolerance of the
     * scheduled fire time — not of some later delivery instant.
     */
    public void testFireStampWithinTolerance() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            long t0wallMs = System.currentTimeMillis();
            GoTimerT t = new GoTimerT(GoTimer.newTimer(100_000_000L), false, null, TIME_DESC);
            long stamped = recvStamp(t);
            long t0nanos = t0wallMs * 1_000_000L;
            // Fire at ~100ms after arm: 10ms slack below for ctor skew,
            // generous 2s above for loaded CI.
            assertInRange(stamped, t0nanos + 90_000_000L, t0nanos + 2_100_000_000L,
                    "fire stamp within tolerance of scheduled fire time");
            // One-shot fired: the translator exits on its own; the
            // withScheduler safety net asserts no translator survives.
        }));
    }

    /**
     * The stamp carries nanosecond resolution: the old code stamped
     * {@code currentTimeMillis() * 1e6}, always divisible by 1e6. At
     * least one of three ticker stamps must have a sub-millisecond part
     * (deterministic red on the old code; 1e-18 flake on the new).
     */
    public void testFireStampNanosecondResolution() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            GoTickerT t = new GoTickerT(GoTimer.newTicker(25_000_000L), TIME_DESC);
            boolean sawSubMs = false;
            for (int i = 0; i < 3; i++) {
                long stamped = recvStamp(t);
                if (stamped % 1_000_000L != 0) {
                    sawSubMs = true;
                }
            }
            assertTrue(t.stop(), "ticker stop -> true");
            assertTrue(sawSubMs,
                    "at least one tick stamp has sub-millisecond resolution");
        }));
    }

    /**
     * Stamps track the monotonic clock: consecutive ticker stamps differ
     * by the true elapsed period (not by delivery-time millis), so
     * {@code time.Since} on them is monotonic-accurate.
     */
    public void testStampsTrackMonotonicElapsed() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            GoTickerT t = new GoTickerT(GoTimer.newTicker(50_000_000L), TIME_DESC);
            long s1 = recvStamp(t);
            long s2 = recvStamp(t);
            long s3 = recvStamp(t);
            assertTrue(t.stop(), "ticker stop -> true");
            // True elapsed between fires is one period; bounds are wide
            // for loaded CI but tight enough to pin the mechanism.
            assertInRange(s2 - s1, 30_000_000L, 500_000_000L, "tick 1->2 elapsed");
            assertInRange(s3 - s2, 30_000_000L, 500_000_000L, "tick 2->3 elapsed");
        }));
    }
}
