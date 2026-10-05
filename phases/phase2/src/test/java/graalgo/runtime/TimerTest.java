package graalgo.runtime;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.sched.SchedulerCore;

/**
 * P2-M3 timer unit tests (WS C; GG-SCHED-0004).
 *
 * <p>Covers: sleep parks (slot release at GOMAXPROCS=1), sleep(0)
 * fast path, After/Timer/Ticker delivery, Stop/Reset edge semantics
 * (Go 1.23+ drain rule, verified vs gc 1.27.1 — see corpus/timers).
 * Differential output-order tests live in
 * {@code graalgo.sched.TimerDifferentialTest}.
 *
 * <p>verifies: GG-SCHED-0004 (timer slice)
 */
public final class TimerTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object e, Object a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private SchedulerCore sched;

    private void withScheduler(int gmp, ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    /** Non-blocking drain count (test scaffolding; never parks). */
    private static int drain(GoChan ch, SchedulerCore sched) {
        int n = 0;
        while (ch.tryRecv(sched) != null) {
            n++;
        }
        return n;
    }

    // Sleep parks the goroutine and releases its slot: at GOMAXPROCS=1 a
    // sleeping peer must not starve a busy one. Mutation (sleep via
    // Thread.sleep, i.e. without parkCurrent) leaves inflight=1 and the
    // busy goroutine never runs → counter stays 0 → red.
    // verifies: GG-SCHED-0004 (P2-D4 falsifier)
    public void testSleepReleasesSlot() throws Exception {
        AtomicLong counter = new AtomicLong();
        AtomicBoolean stop = new AtomicBoolean();
        withScheduler(1, () -> {
            sched.spawn(() -> Timers.sleep(800_000_000L));
            sched.spawn(() -> {
                while (!stop.get()) {
                    counter.incrementAndGet();
                }
            });
            Thread.sleep(300); // sleeper still parked (800ms)
            long c = counter.get();
            assertTrue(c > 1000,
                    "busy goroutine starved at GOMAXPROCS=1 while peer slept: " + c);
            stop.set(true);
        });
        assertTrue(sched.maxInflightObserved() <= 1,
                "cap violated: maxInflight=" + sched.maxInflightObserved());
    }

    // Sleep(0)/Sleep(-1) return immediately and enqueue no timer.
    public void testSleepZeroImmediate() throws Exception {
        AtomicBoolean done = new AtomicBoolean();
        withScheduler(1, () -> {
            sched.spawn(() -> {
                Timers.sleep(0);
                Timers.sleep(-1);
                done.set(true);
            });
        });
        assertTrue(done.get(), "sleep(0) goroutine did not complete");
        assertTrue(sched.timerHeap().pendingCount() == 0,
                "sleep(0) enqueued a timer");
    }

    // Three sleeps fire in deadline order.
    public void testSleepOrder() throws Exception {
        StringBuilder log = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                Timers.sleep(150_000_000L);
                synchronized (log) {
                    log.append("slow\n");
                }
            });
            sched.spawn(() -> {
                Timers.sleep(50_000_000L);
                synchronized (log) {
                    log.append("fast\n");
                }
            });
            sched.spawn(() -> {
                Timers.sleep(100_000_000L);
                synchronized (log) {
                    log.append("mid\n");
                }
            });
        });
        assertEquals("fast\nmid\nslow\n", log.toString(), "sleep order");
    }

    // time.After delivers on its channel.
    public void testAfterFires() throws Exception {
        StringBuilder log = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = Timers.after(50_000_000L);
            sched.spawn(() -> {
                ChanOps.recv(ch);
                log.append("fired\n");
            });
        });
        assertEquals("fired\n", log.toString(), "after");
    }

    // Stop on an active timer: true, and it never fires.
    public void testTimerStopActive() throws Exception {
        withScheduler(1, () -> {
            GoTimer t = GoTimer.newTimer(200_000_000L);
            assertTrue(t.stop(), "stop on active timer");
            Thread.sleep(300);
            assertTrue(drain((GoChan) t.channel(), sched) == 0,
                    "stopped timer fired");
            assertTrue(!t.stop(), "second stop");
        });
    }

    // Stop on an expired+undrained timer (Go 1.23+): true, pending value
    // drained.
    public void testTimerStopUndrained() throws Exception {
        withScheduler(1, () -> {
            GoTimer t = GoTimer.newTimer(20_000_000L);
            Thread.sleep(150); // fired, not drained
            assertTrue(t.stop(), "stop on expired-undrained");
            assertTrue(drain((GoChan) t.channel(), sched) == 0,
                    "stopped timer leaked its value");
        });
    }

    // Stop on an expired+drained timer: false.
    public void testTimerStopDrained() throws Exception {
        withScheduler(1, () -> {
            GoTimer t = GoTimer.newTimer(20_000_000L);
            Thread.sleep(150);
            assertTrue(drain((GoChan) t.channel(), sched) == 1, "drain");
            assertTrue(!t.stop(), "stop on expired+drained");
        });
    }

    // Reset on an active timer: true, fires at the new deadline.
    public void testTimerResetActive() throws Exception {
        StringBuilder log = new StringBuilder();
        withScheduler(2, () -> {
            GoTimer t = GoTimer.newTimer(500_000_000L);
            assertTrue(t.reset(30_000_000L), "reset on active");
            sched.spawn(() -> {
                ChanOps.recv(t.channel());
                log.append("fired\n");
            });
        });
        assertEquals("fired\n", log.toString(), "reset-active fire");
    }

    // Reset on an expired+drained timer: false, fires at the new deadline.
    public void testTimerResetExpiredDrained() throws Exception {
        StringBuilder log = new StringBuilder();
        withScheduler(2, () -> {
            GoTimer t = GoTimer.newTimer(20_000_000L);
            Thread.sleep(150);
            assertTrue(drain((GoChan) t.channel(), sched) == 1, "drain");
            assertTrue(!t.reset(30_000_000L), "reset on expired+drained");
            sched.spawn(() -> {
                ChanOps.recv(t.channel());
                log.append("fired\n");
            });
        });
        assertEquals("fired\n", log.toString(), "reset-expired fire");
    }

    // Reset on an expired+undrained timer (Go 1.23+): true, stale value
    // dropped, exactly one new fire.
    public void testTimerResetExpiredUndrained() throws Exception {
        withScheduler(1, () -> {
            GoTimer t = GoTimer.newTimer(20_000_000L);
            Thread.sleep(150); // fired, not drained
            assertTrue(t.reset(50_000_000L), "reset on expired-undrained");
            Thread.sleep(150); // new deadline passes
            assertTrue(drain((GoChan) t.channel(), sched) == 1,
                    "expected exactly one (new) value, stale must be dropped");
        });
    }

    // Ticker delivers repeated ticks; Stop ends them.
    public void testTickerFires() throws Exception {
        StringBuilder log = new StringBuilder();
        withScheduler(2, () -> {
            GoTimer tk = GoTimer.newTicker(50_000_000L);
            sched.spawn(() -> {
                for (int i = 0; i < 3; i++) {
                    ChanOps.recv(tk.channel());
                    log.append("tick\n");
                }
                tk.stop();
                log.append("stopped\n");
            });
        });
        assertEquals("tick\ntick\ntick\nstopped\n", log.toString(), "ticker");
    }

    // Slow ticker receiver: ticks are dropped, never queued.
    public void testTickerDropsSlowReceiver() throws Exception {
        withScheduler(1, () -> {
            GoTimer tk = GoTimer.newTicker(20_000_000L);
            Thread.sleep(300); // ~15 periods pass, receiver absent
            int n = drain((GoChan) tk.channel(), sched);
            tk.stop();
            assertTrue(n >= 1 && n <= 2,
                    "expected drops (1-2 buffered), got " + n);
        });
    }

    // NewTicker(0) panics, gc message parity.
    public void testTickerZeroPanics() throws Exception {
        withScheduler(1, () -> {
            AtomicReference<String> msg = new AtomicReference<>();
            sched.spawn(() -> {
                try {
                    GoTimer.newTicker(0);
                } catch (graalgo.runtime.PanicException pe) {
                    msg.set(String.valueOf(pe.goValue()));
                }
            });
            sched.awaitQuiescence();
            assertEquals("non-positive interval for NewTicker", msg.get(),
                    "ticker panic message");
        });
    }

    // Frozen startTimer/stopTimer pair: arm, cancel before fire.
    public void testStartStopTimerFrozen() throws Exception {
        AtomicBoolean fired = new AtomicBoolean();
        withScheduler(1, () -> {
            long id = Timers.startTimerId(
                    System.nanoTime() + 500_000_000L,
                    () -> fired.set(true));
            Timers.stopTimer(id);
            Thread.sleep(700);
        });
        assertTrue(!fired.get(), "cancelled startTimer fired");
    }

    // startTimer fires and runs the callback (on the timer thread, gc
    // timer-proc parity — the callback must not block).
    public void testStartTimerFires() throws Exception {
        AtomicBoolean ran = new AtomicBoolean();
        withScheduler(1, () -> {
            Timers.startTimer(System.nanoTime() + 50_000_000L,
                    () -> ran.set(true));
            Thread.sleep(300);
        });
        assertTrue(ran.get(), "startTimer callback did not run");
    }
}
