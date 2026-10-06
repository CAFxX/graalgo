package graalgo.p3time;

import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.GoTimer;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Unit tests for the P3-M5 time runtime (ADR-0005).
 *
 * <p>{@link #testTimerChanLenCapAllStates} is the Java-level regression
 * for DIV-M3-1: {@code len}/{@code cap} (i.e. {@code buffered()} /
 * {@code capacity()}) of a timer channel report 0 in every state —
 * pre-fire, fired-undrained, drained, stopped — while fire values stay
 * deliverable. (The corpus tc07 covers it end to end; this pins it at
 * the unit level.)
 *
 * <p>The remaining tests pin the Stop/Reset gc-parity table and the
 * quiescence-safety property every translator must have: after
 * {@code stop()}/shutdown no translator goroutine may remain parked,
 * otherwise {@code awaitQuiescence} would hang program exit.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeChanLenCapTest {

    private static final GoStructDescriptor TIME_DESC = GoStructDescriptor.layout(
            "time.Time", List.of("unixnano"), List.of(GoKind.INT64));

    // ---------- scaffolding ----------

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

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

    /**
     * Non-blocking receive-readiness probe (consumes the value if ready —
     * callers use it only where consumption is intended or the channel is
     * known empty).
     */
    private static boolean isRecvReady(GoChan ch) {
        int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                new ChanOps.SelectCase(ch, false, null),
                new ChanOps.SelectCase(null, false, null),
        });
        return idx == 0;
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

    private static void assertLenCapZero(TimeChan ch, String state) {
        assertEq(0, ch.buffered(), state + ": buffered()");
        assertEq(0, ch.capacity(), state + ": capacity()");
    }

    // ---------- tests ----------

    /** DIV-M3-1, all states, plus the Stop/Reset parity table around them. */
    public void testTimerChanLenCapAllStates() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            GoTimerT t = new GoTimerT(GoTimer.newTimer(50_000_000L), false, null, TIME_DESC);
            TimeChan ch = t.userChannel();
            assertLenCapZero(ch, "pre-fire");

            // Fire at 50ms; the translator delivers within microseconds.
            Thread.sleep(150);
            assertLenCapZero(ch, "fired-undrained");

            // gc 1.23+: Stop on fired-undrained drains and reports true.
            assertTrue(t.stop(), "stop on fired-undrained -> true");
            assertLenCapZero(ch, "stopped");
            assertFalse(isRecvReady(ch), "stop drained the pending value");
            assertFalse(t.stop(), "second stop -> false");

            // Reset on a stopped timer re-arms but reports false.
            assertFalse(t.reset(50_000_000L), "reset on stopped -> false");
            GoChan.RecvResult r = ch.recv();
            assertTrue(r.value() instanceof GoStruct,
                    "fire value is a Time struct, got " + r.value());
            long nanos = ((GoStruct) r.value()).getLong(0);
            long nowMs = System.currentTimeMillis();
            assertTrue(Math.abs(nanos / 1_000_000L - nowMs) < 60_000,
                    "fire Time is ~now, got " + nanos);
            assertLenCapZero(ch, "drained");
            assertFalse(t.stop(), "stop on fired+drained -> false");

            waitUntil(() -> P3TimeRuntime.backgroundLive() == 0, 5_000,
                    "translator exited after stop");
        }));
    }

    /** Ticker: three Time ticks, len/cap 0, Stop drains and reports true. */
    public void testTickerThreeTicksAndStop() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            GoTickerT t = new GoTickerT(GoTimer.newTicker(20_000_000L), TIME_DESC);
            TimeChan ch = t.userChannel();
            for (int i = 0; i < 3; i++) {
                GoChan.RecvResult r = ch.recv();
                assertTrue(r.value() instanceof GoStruct, "tick " + i + " is a Time struct");
                assertLenCapZero(ch, "tick " + i);
            }
            assertTrue(t.stop(), "ticker stop -> true");
            assertFalse(isRecvReady(ch), "no tick delivered after stop");
            waitUntil(() -> P3TimeRuntime.backgroundLive() == 0, 5_000,
                    "ticker translator exited after stop");
        }));
    }

    /** Stop before the fire: true, and no value is ever delivered. */
    public void testStopBeforeFirePreventsDelivery() throws Exception {
        withScheduler(() -> inGoroutine(SchedulerCore.current(), () -> {
            GoTimerT t = new GoTimerT(GoTimer.newTimer(5_000_000_000L), false, null, TIME_DESC);
            assertTrue(t.stop(), "stop before fire -> true");
            Thread.sleep(100);
            assertFalse(isRecvReady(t.userChannel()), "no delivery after stop");
            waitUntil(() -> P3TimeRuntime.backgroundLive() == 0, 5_000,
                    "translator exited after stop");
        }));
    }

    /**
     * Program-end path: a live (never stopped) ticker's translator must
     * exit on {@link P3TimeRuntime#shutdownBackground} so
     * {@code awaitQuiescence} completes — the two-phase launcher
     * protocol for {@code time.Tick}.
     */
    public void testShutdownBackgroundEndsLiveTicker() throws Exception {
        withScheduler(() -> {
            inGoroutine(SchedulerCore.current(), () -> {
                GoTickerT t = new GoTickerT(GoTimer.newTicker(10_000_000L), TIME_DESC);
                GoChan.RecvResult r = t.userChannel().recv();
                assertTrue(r.value() instanceof GoStruct, "tick is a Time struct");
                // Deliberately not stopped: gc's Tick leaks; the launcher
                // shuts the translator down at program end.
            });
            P3TimeRuntime.shutdownBackground();
            waitUntil(() -> P3TimeRuntime.backgroundLive() == 0, 5_000,
                    "ticker translator exited on shutdown");
        });
    }
}
