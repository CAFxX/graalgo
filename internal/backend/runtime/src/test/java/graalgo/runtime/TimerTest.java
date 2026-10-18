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

    private static void assertFalse(boolean c, String msg) {
        if (c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertNull(Object o, String msg) {
        if (o != null) {
            throw new AssertionError(msg + " expected=null actual=" + o);
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

    /**
     * Non-blocking drain count (test scaffolding; never parks).
     * P0-1: holds the channel monitor — tryRecv is a monitor-held
     * primitive and this runs concurrently with the timer thread.
     */
    private static int drain(GoChan ch, SchedulerCore sched) {
        int n = 0;
        synchronized (ch) {
            while (ch.tryRecv(sched) != null) {
                n++;
            }
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

    // Three sleeps fire in deadline order. P1-2: separations ≥300ms;
    // 50ms sat inside the measured fire-lateness noise band and flaked.
    public void testSleepOrder() throws Exception {
        StringBuilder log = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                Timers.sleep(900_000_000L);
                synchronized (log) {
                    log.append("slow\n");
                }
            });
            sched.spawn(() -> {
                Timers.sleep(300_000_000L);
                synchronized (log) {
                    log.append("fast\n");
                }
            });
            sched.spawn(() -> {
                Timers.sleep(600_000_000L);
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
                // P2-8: the old 500ms deadline must have been cancelled —
                // no second fire may arrive.
                try {
                    Thread.sleep(700);
                } catch (InterruptedException ie) {
                    // ignore
                }
                int extra;
                GoChan ch = (GoChan) t.channel();
                synchronized (ch) {
                    extra = 0;
                    while (ch.tryRecv(sched) != null) {
                        extra++;
                    }
                }
                log.append("extra:").append(extra).append("\n");
            });
        });
        assertEquals("fired\nextra:0\n", log.toString(), "reset-active fire");
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

    // P0-2: stop() on a live ticker with a buffered tick drains it
    // (gc timerchandrain parity) — no stale tick is observable after
    // Stop. Red-green: fails on the pre-fix code (buffered()==1).
    public void testTickerStopDrainsBufferedTick() throws Exception {
        withScheduler(1, () -> {
            GoTimer tk = GoTimer.newTicker(20_000_000L);
            Thread.sleep(300); // ticks fire; exactly 1 buffered (cap-1)
            assertTrue(tk.stop(), "stop on live ticker with buffered tick");
            assertEquals(0, drain((GoChan) tk.channel(), sched),
                    "no stale tick after stop");
            assertFalse(tk.stop(), "second stop reports inactive");
        });
    }

    // P0-2: reset() drains a stale buffered tick before re-arming, so
    // the next receive is a fresh post-reset tick, not the stale one.
    // Red-green: fails on the pre-fix code (stale tick received first).
    // Polls with the non-blocking drain (the test thread is not a
    // scheduler goroutine, so it cannot block in recv).
    public void testTickerResetDrainsStaleTick() throws Exception {
        withScheduler(1, () -> {
            GoTimer tk = GoTimer.newTicker(20_000_000L);
            Thread.sleep(300); // 1 tick buffered
            long tReset = System.nanoTime();
            assertTrue(tk.reset(80_000_000L), "reset on live ticker");
            GoChan ch = (GoChan) tk.channel();
            long tRecv = 0;
            for (int i = 0; i < 200 && tRecv == 0; i++) {
                if (drain(ch, sched) > 0) {
                    tRecv = System.nanoTime();
                } else {
                    Thread.sleep(10);
                }
            }
            tk.stop();
            assertTrue(tRecv > 0, "tick received after reset");
            // A stale tick would arrive immediately (dt ~ 0); the fresh
            // post-reset tick arrives ~80ms after reset. 40ms threshold
            // is far from both (timers never fire early).
            assertTrue(tRecv - tReset > 40_000_000L,
                    "fresh post-reset tick, not stale; dt=" + (tRecv - tReset) + "ns");
        });
    }

    // P0-2 (one-shot): stop() after fire but before receive drains the
    // pending value and returns true (Go 1.23+ parity).
    public void testTimerStopAfterFireDrains() throws Exception {
        withScheduler(1, () -> {
            GoTimer t = GoTimer.newTimer(20_000_000L);
            Thread.sleep(150); // fired, undrained
            assertTrue(t.stop(), "stop after fire, undrained");
            assertEquals(0, drain((GoChan) t.channel(), sched),
                    "pending value drained by stop");
        });
    }

    // Stress/regression pin: after stop() returns, no tick is ever
    // delivered again. (Not a red-green falsifier — the pre-fix re-arm
    // race it names could not occur; the real pre-fix stale-tick path
    // was P0-3's unlocked send, a nanosecond window no stress test
    // reliably exercises. Kept as a regression net.)
    public void testNoTickAfterStopStress() throws Exception {
        withScheduler(2, () -> {
            for (int i = 0; i < 300; i++) {
                GoTimer tk = GoTimer.newTicker(1_000_000L);
                Thread.sleep(5); // several ticks fire
                tk.stop();
                drain((GoChan) tk.channel(), sched);
                Thread.sleep(10); // let any in-flight send land
                assertEquals(0, drain((GoChan) tk.channel(), sched),
                        "stale tick after stop; iter=" + i);
            }
        });
    }

    // P1-2: time.Tick(0) returns null (gc parity); NewTicker still panics.
    public void testTickZeroReturnsNull() throws Exception {
        withScheduler(1, () -> {
            assertNull(GoTimer.tick(0), "tick(0)");
            assertNull(GoTimer.tick(-5_000_000L), "tick(-)");
        });
    }

    // P1-3: Ticker.Reset(0) panics (gc message parity); Timer.Reset(0)
    // is allowed and fires immediately.
    public void testTickerResetZeroPanics() throws Exception {
        withScheduler(1, () -> {
            AtomicReference<String> msg = new AtomicReference<>();
            sched.spawn(() -> {
                GoTimer tk = GoTimer.newTicker(50_000_000L);
                try {
                    tk.reset(0);
                } catch (graalgo.runtime.PanicException pe) {
                    msg.set(String.valueOf(pe.goValue()));
                }
                tk.stop();
            });
            sched.awaitQuiescence();
            assertEquals("non-positive interval for Ticker.Reset", msg.get(),
                    "ticker reset panic message");
        });
    }

    public void testTimerResetZeroFiresImmediately() throws Exception {
        withScheduler(1, () -> {
            GoTimer t = GoTimer.newTimer(60_000_000L);
            assertTrue(t.reset(0), "timer reset(0) allowed");
            // Poll for the fire (non-blocking; test thread is not a goroutine).
            GoChan ch = (GoChan) t.channel();
            boolean got = false;
            for (int i = 0; i < 100 && !got; i++) {
                if (drain(ch, sched) > 0) {
                    got = true;
                } else {
                    Thread.sleep(10);
                }
            }
            t.stop();
            assertTrue(got, "timer fired after reset(0)");
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

    // P2-7: pin the b3a7546 headline — the startTimer callback runs on
    // the timer thread (gc timer-proc parity), not a carrier.
    public void testStartTimerCallbackOnTimerThread() throws Exception {
        AtomicReference<String> threadName = new AtomicReference<>();
        withScheduler(1, () -> {
            Timers.startTimer(System.nanoTime() + 50_000_000L,
                    () -> threadName.set(Thread.currentThread().getName()));
            Thread.sleep(300);
        });
        assertEquals("graalgo-timer", threadName.get(),
                "callback thread");
    }

    // P2-7: a throwing timer callback does not kill the timer thread —
    // the next timer still fires.
    public void testThrowingCallbackDoesNotKillTimerThread() throws Exception {
        AtomicBoolean secondRan = new AtomicBoolean();
        withScheduler(1, () -> {
            Timers.startTimer(System.nanoTime() + 50_000_000L,
                    () -> { throw new RuntimeException("boom"); });
            Timers.startTimer(System.nanoTime() + 150_000_000L,
                    () -> secondRan.set(true));
            Thread.sleep(400);
        });
        assertTrue(secondRan.get(),
                "timer thread died after throwing callback");
    }

    // P2-7: Netpoll deletion pin — all five methods throw UOE with the
    // GG-0018 message (netpoller deleted; blocking I/O uses ioEnter/ioExit).
    public void testNetpollDeletion() throws Exception {
        withScheduler(1, () -> {
            assertThrowsUoe(() -> Netpoll.init());
            assertThrowsUoe(() -> Netpoll.open(0, 0));
            assertThrowsUoe(() -> Netpoll.close(0));
            assertThrowsUoe(() -> Netpoll.waitForEvents(true));
            assertThrowsUoe(() -> Netpoll.breakWait());
        });
    }

    // P1-4: drainLocked must terminate on a closed channel. Pre-fix, the
    // loop `while (ch.tryRecv(sched) != null)` never exits once the channel
    // is closed (tryRecv returns (zeroValue, false) — non-null — forever),
    // wedging the timer thread while holding the heap lock, so all timers
    // stop firing. Closing a timer channel is forbidden by Go's type
    // system (t.C is <-chan); the runtime must terminate anyway rather
    // than wedge on the external invariant.
    // Red-green: on the pre-fix code the stop() below never returns; the
    // join timeout turns that into a test failure instead of hanging the
    // runner (the worker is a daemon, so the JVM can still exit).
    public void testStopOnClosedChannelTerminates() throws Exception {
        withScheduler(1, () -> {
            GoTimer t = GoTimer.newTimer(20_000_000L);
            Thread.sleep(150); // fired; stale value buffered
            GoChan ch = (GoChan) t.channel();
            ChanOps.close(t.channel());
            AtomicReference<Boolean> stopped = new AtomicReference<>();
            AtomicReference<Throwable> err = new AtomicReference<>();
            Thread w = new Thread(() -> {
                try {
                    stopped.set(t.stop());
                } catch (Throwable th) {
                    err.set(th);
                }
            });
            w.setDaemon(true);
            w.start();
            w.join(10_000);
            assertFalse(w.isAlive(),
                    "stop() wedged: drainLocked looped forever on the closed channel");
            assertNull(err.get(), "stop() threw: " + err.get());
            // The stale pre-close value was drained (not left behind):
            // stop reports active, gc timerchandrain parity.
            assertEquals(Boolean.TRUE, stopped.get(),
                    "stop on fired-undrained closed channel");
            // Channel is closed and fully drained: tryRecv yields
            // (zero, ok=false), never a stale value.
            GoChan.RecvResult rr;
            synchronized (ch) {
                rr = ch.tryRecv(sched);
            }
            assertTrue(rr != null && !rr.ok(),
                    "closed+drained channel must report ok=false");
        });
    }

    private static void assertThrowsUoe(Runnable r) {
        try {
            r.run();
        } catch (UnsupportedOperationException uoe) {
            assertTrue(String.valueOf(uoe.getMessage()).contains("GG-0018"),
                    "UOE message must cite GG-0018: " + uoe.getMessage());
            return;
        }
        throw new AssertionError("expected UnsupportedOperationException");
    }
}
