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
 * Unit tests for P1-27: {@link GoTimerT} must stamp the user-visible
 * {@code time.Time} from the heap's monotonic fire nanotime (converted
 * to wall time via the creation-time clock base pair), not from the
 * wall clock sampled at translator-delivery time.
 *
 * <p>Scaffolding mirrors {@link TimeChanLenCapTest} (self-contained
 * test classes are the local convention).
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class GoTimerFireTimeTest {

    private static final GoStructDescriptor TIME_DESC = GoStructDescriptor.layout(
            "time.Time", List.of("unixnano", "isSet"), List.of(GoKind.INT64, GoKind.BOOL));

    // ---------- scaffolding ----------

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
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
            assertEq(0, P3TimeRuntime.backgroundLive(), "background translators at end");
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- tests ----------

    /**
     * The monotonic→wall conversion is exact: base wall plus the
     * monotonic delta, nanosecond-precise, no truncation of the delta.
     */
    public void testConversionExact() {
        long wallBase = 1_767_225_600_000L; // a fixed wall base (ms)
        long monoBase = 9_000_000_000L; // a fixed monotonic base (ns)
        assertEq(wallBase * 1_000_000L,
                GoTimerT.fireNanosToWallNanos(wallBase, monoBase, monoBase),
                "zero delta -> exactly the base wall");
        assertEq(wallBase * 1_000_000L + 1_500_000_000L,
                GoTimerT.fireNanosToWallNanos(wallBase, monoBase, monoBase + 1_500_000_000L),
                "1.5s delta is ns-exact");
        assertEq(wallBase * 1_000_000L + 3_600_000_000_000L,
                GoTimerT.fireNanosToWallNanos(wallBase, monoBase, monoBase + 3_600_000_000_000L),
                "1h delta: exact, no overflow");
        // Sub-millisecond deltas survive (the old stamping truncated to ms).
        assertEq(wallBase * 1_000_000L + 123_456L,
                GoTimerT.fireNanosToWallNanos(wallBase, monoBase, monoBase + 123_456L),
                "123us delta is ns-exact");
    }

    /**
     * P1-27 RED probe: the translator must honor the heap's fire
     * nanotime. A synthetic fire value 10 s after the clock base is
     * injected on the internal channel; the delivered Time's unixnano
     * must equal the converted fire value EXACTLY. Pre-fix the
     * translator stamps delivery-time wall (~base wall), so this fails
     * by ~10 s. Fully deterministic — no timing involved.
     */
    public void testTranslatorHonorsFireNanos() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            // A timer that never fires during the test; the fire value
            // is injected deterministically instead.
            GoTimerT t = new GoTimerT(GoTimer.newTimer(30_000_000_000L), false, null, TIME_DESC);
            long monoBase = t.monoBaseNanosForTest();
            long wallBase = t.wallBaseMillisForTest();
            long fireNanos = monoBase + 10_000_000_000L; // 10 s after the base
            long expected = GoTimerT.fireNanosToWallNanos(wallBase, monoBase, fireNanos);

            t.injectFireNanosForTest(fireNanos);

            GoChan.RecvResult r = t.userChannel().recv();
            assertTrue(r.value() instanceof GoStruct,
                    "delivered value is a Time struct, got " + r.value());
            long got = ((GoStruct) r.value()).getLong(0);
            assertEq(expected, got,
                    "translator must stamp the heap fire nanotime, not delivery-time wall");

            assertTrue(t.stop(), "stop after injected fire still reports the pending heap timer");
        }));
    }

    /**
     * End-to-end sanity through the real heap: a 50 ms timer's
     * delivered Time is within [arm, now]. Passes pre- and post-fix
     * (regression net for the translation path, not the RED probe).
     */
    public void testDeliveredValueIsSane() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            long armWallMs = System.currentTimeMillis();
            GoTimerT t = new GoTimerT(GoTimer.newTimer(50_000_000L), false, null, TIME_DESC);
            GoChan.RecvResult r = t.userChannel().recv();
            assertTrue(r.value() instanceof GoStruct,
                    "fire value is a Time struct, got " + r.value());
            long got = ((GoStruct) r.value()).getLong(0);
            long nowWallMs = System.currentTimeMillis();
            assertTrue(got / 1_000_000L >= armWallMs,
                    "fire time not before arm, got " + got);
            assertTrue(got / 1_000_000L <= nowWallMs + 5,
                    "fire time not after now, got " + got);
        }));
    }

    /**
     * P1-27: a timer re-armed via {@code reset()} still stamps from the
     * heap's fire nanotime. End-to-end through the real heap: arm long,
     * reset to 50 ms, receive, and bound the delivered value by the
     * reset-time wall clock (regression net for the reset path — the
     * base pair is captured once at construction and reused across
     * re-arms, so the delta stays non-negative and exact).
     */
    public void testResetTimerStampsCorrectly() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            long armWallMs = System.currentTimeMillis();
            GoTimerT t = new GoTimerT(GoTimer.newTimer(30_000_000_000L), false, null, TIME_DESC);
            t.reset(50_000_000L);
            GoChan.RecvResult r = t.userChannel().recv();
            assertTrue(r.value() instanceof GoStruct,
                    "fire value is a Time struct, got " + r.value());
            long got = ((GoStruct) r.value()).getLong(0);
            long nowWallMs = System.currentTimeMillis();
            assertTrue(got / 1_000_000L >= armWallMs,
                    "reset fire time not before arm, got " + got);
            assertTrue(got / 1_000_000L <= nowWallMs + 5,
                    "reset fire time not after now, got " + got);
        }));
    }
}
