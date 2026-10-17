package graalgo.sched;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.jfr.ParkReason;
import graalgo.runtime.GoChan;

/**
 * RV-P1-19 deadlock-detector tests (design §6.1): deterministic,
 * in-JVM, no wall-clock sleeps on assertion paths.
 *
 * <p>Test-writing rules (see {@link SchedTestRunner}): goroutines never
 * JVM-block; they spin on volatile flags with
 * {@link SchedulerCore#yieldCurrent} (scheduler-aware, never enters the
 * parked map) until the test thread has finished spawning — closing the
 * foreign-spawn burst window in which the detector cannot distinguish
 * "about to spawn" from "deadlocked". Latches are awaited on the test
 * thread only.
 *
 * <p>Red-green: every positive test fails (hangs in
 * {@code awaitQuiescence} or misses the report) without the detector;
 * every negative test fails (spurious {@link DeadlockException}) if its
 * predicate clause is dropped.
 *
 * <p>verifies: RV-P1-19 (deadlock detector).
 */
public final class DeadlockDetectTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertFalse(boolean cond, String msg) {
        assertTrue(!cond, msg);
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Bounded poll on the test thread; fails loudly instead of hanging. */
    private static void waitFor(java.util.function.BooleanSupplier cond, String what)
            throws Exception {
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(15);
        while (!cond.getAsBoolean()) {
            if (System.nanoTime() > deadline) {
                throw new AssertionError("timed out waiting for: " + what);
            }
            Thread.sleep(1);
        }
    }

    private static SchedulerCore newScheduler() {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        // These tests exercise the detector itself: arm it. (It defaults
        // to disarmed so other features' unit tests — which routinely
        // stage transient all-parked states — are unaffected.)
        sched.setDeadlockDetectionEnabled(true);
        SchedulerCore.install(sched);
        return sched;
    }

    // ---------- 1: positive — single parked goroutine ----------

    /**
     * One goroutine parked in {@code select}: {@code awaitQuiescence}
     * throws {@link DeadlockException} with the gc-identical first line
     * and a per-goroutine dump entry.
     */
    public void testSingleParkedGoroutineDeadlocks() throws Exception {
        SchedulerCore sched = newScheduler();
        try {
            GoroutineHandle h = sched.spawn(
                    () -> sched.parkCurrent(null, ParkReason.SELECT));
            try {
                sched.awaitQuiescence();
                throw new AssertionError("expected DeadlockException");
            } catch (DeadlockException de) {
                String report = de.getReport();
                assertTrue(
                        report.startsWith(
                                "fatal error: all goroutines are asleep - deadlock!\n"),
                        "gc-identical first line, got:\n" + report);
                assertTrue(report.contains("goroutine " + h.id + " [select]:"),
                        "dump names the goroutine with the select label, got:\n" + report);
            }
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- 2: positive — two-goroutine channel deadlock ----------

    /**
     * Two goroutines blocked in receive on the same unbuffered channel
     * (receives never rendezvous with each other): deadlock with both
     * ids in the dump, sorted ascending.
     */
    public void testTwoGoroutineChannelDeadlock() throws Exception {
        SchedulerCore sched = newScheduler();
        try {
            GoChan ch = new GoChan(0, null);
            AtomicBoolean release = new AtomicBoolean(false);
            GoroutineHandle h1 = sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                ch.recv();
            });
            GoroutineHandle h2 = sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                ch.recv();
            });
            release.set(true);
            try {
                sched.awaitQuiescence();
                throw new AssertionError("expected DeadlockException");
            } catch (DeadlockException de) {
                String report = de.getReport();
                assertTrue(
                        report.startsWith(
                                "fatal error: all goroutines are asleep - deadlock!\n"),
                        "gc-identical first line, got:\n" + report);
                int i1 = report.indexOf("goroutine " + h1.id + " [chan wait]:");
                int i2 = report.indexOf("goroutine " + h2.id + " [chan wait]:");
                assertTrue(i1 >= 0 && i2 >= 0,
                        "both goroutines in the dump, got:\n" + report);
                assertTrue(i1 < i2, "dump sorted by id ascending, got:\n" + report);
            }
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- 3: negative — pending timer suppresses ----------

    /**
     * A far-future SLEEP timer suppresses the report (gc probe p5): both
     * goroutines parked, timer pending — the post-PARKED drainer
     * evaluation runs with the timer armed, so no deadlock is recorded;
     * after cancelling the timer and unparking both, quiescence returns
     * normally.
     */
    public void testPendingTimerSuppressesDeadlock() throws Exception {
        SchedulerCore sched = newScheduler();
        try {
            AtomicBoolean release = new AtomicBoolean(false);
            AtomicLong timerId = new AtomicLong(-1);
            GoroutineHandle h1 = sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                sched.parkCurrent(null, ParkReason.SELECT);
            });
            GoroutineHandle h2 = sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                timerId.set(sched.timerHeap().addSleep(
                        System.nanoTime() + TimeUnit.HOURS.toNanos(1),
                        sched.currentGoroutine()));
                sched.parkCurrent(null, ParkReason.TIMER);
            });
            release.set(true);
            // Route and the detector check share the drainer critical
            // section: once both parks are routed, the suppressing
            // evaluation has already run.
            waitFor(() -> sched.drainer().parkedCountForTest() == 2, "both parked");
            assertFalse(sched.isDeadlocked(), "pending timer suppresses the report");
            assertTrue(sched.timerHeap().cancel(timerId.get()), "timer was pending");
            sched.unpark(h1);
            sched.unpark(h2);
            sched.awaitQuiescence();
            assertEquals(0, sched.runnableCount(), "quiescence after unpark");
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- 4: negative — in-flight I/O suppresses ----------

    /**
     * A goroutine inside {@code ioEnter}/{@code ioExit} (no syscall —
     * scheduler methods called directly) suppresses the report while
     * another goroutine is parked; after {@code ioExit} and unparking,
     * quiescence returns normally.
     */
    public void testInflightIoSuppressesDeadlock() throws Exception {
        SchedulerCore sched = newScheduler();
        try {
            AtomicBoolean release = new AtomicBoolean(false);
            AtomicBoolean release2 = new AtomicBoolean(false);
            CountDownLatch entered = new CountDownLatch(1);
            GoroutineHandle h1 = sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                sched.parkCurrent(null, ParkReason.SELECT);
            });
            sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                sched.ioEnter();
                entered.countDown();
                while (!release2.get()) {
                    sched.yieldCurrent();
                }
                sched.ioExit();
            });
            release.set(true);
            assertTrue(entered.await(10, TimeUnit.SECONDS), "g2 entered blocking I/O");
            waitFor(() -> sched.drainer().parkedCountForTest() == 1, "g1 parked");
            assertFalse(sched.isDeadlocked(), "in-flight I/O suppresses the report");
            release2.set(true);
            sched.unpark(h1);
            sched.awaitQuiescence();
            assertEquals(0, sched.runnableCount(), "quiescence after ioExit+unpark");
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- 5: negative — spawn race (white-box) ----------

    /**
     * A foreign-thread spawn in progress ({@code spawnInflight > 0})
     * suppresses an otherwise-true predicate (design §4.2(4)). The real
     * {@code spawn} holds the counter across the
     * vthread.start()→INIT-offer window, which no test can pause inside
     * deterministically, so the test holds it via the test-only
     * adjuster; counter discipline of real spawns is asserted too.
     */
    public void testSpawnInflightSuppressesDeadlock() throws Exception {
        SchedulerCore sched = newScheduler();
        try {
            AtomicBoolean release = new AtomicBoolean(false);
            // Simulate a foreign spawn in progress across the whole test.
            sched.addSpawnInflightForTest(1);
            GoroutineHandle h1 = sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                sched.parkCurrent(null, ParkReason.SELECT);
            });
            assertEquals(1, sched.spawnInflightCount(),
                    "real spawn() balances its increment around the hold");
            release.set(true);
            waitFor(() -> sched.drainer().parkedCountForTest() == 1, "g1 parked");
            assertFalse(sched.isDeadlocked(), "spawn in flight suppresses the report");
            // Release the hold; force a re-evaluation with a quick
            // exiter's EXIT token — the detector must fire now.
            sched.addSpawnInflightForTest(-1);
            assertEquals(0, sched.spawnInflightCount(), "hold released");
            sched.spawn(() -> {});
            waitFor(sched::isDeadlocked, "detector fires once the hold is released");
            try {
                sched.awaitQuiescence();
                throw new AssertionError("expected DeadlockException");
            } catch (DeadlockException de) {
                assertTrue(de.getReport().startsWith(
                        "fatal error: all goroutines are asleep - deadlock!\n"),
                        "gc-identical first line, got:\n" + de.getReport());
            } finally {
                sched.unpark(h1);
            }
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- 6: negative — UNPARK-before-PARKED (white-box) ----------

    /**
     * UNPARK offered before its PARKED is routed must not lose the
     * wakeup: driven deterministically via
     * {@code processOneForTest} (drainer thread down — the scheduler is
     * deliberately not started). The goroutine is admitted through the
     * WAKE lane and no deadlock is declared.
     */
    public void testUnparkBeforeParkedWhiteBox() throws Exception {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        Drainer d = sched.drainer();
        List<GoroutineHandle> admitted = new ArrayList<>();
        d.setAdmissionListener(admitted::add);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = new Thread(); // dummy: admit() unparks it; never started
        d.setInflightForTest(1); // the fake goroutine holds a slot
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.PARKED, h, 0));
        assertTrue(admitted.contains(h), "stashed wakeup admits the goroutine");
        assertEquals(0, d.parkedCountForTest(), "not left parked");
        assertFalse(sched.isDeadlocked(), "no deadlock declared");
    }

    // ---------- 7: falsification — timer-fire storm at the boundary ----------

    /**
     * 1000 iterations on one scheduler: a goroutine arms a 1ns SLEEP
     * timer <em>before</em> parking. At every drainer evaluation the
     * timer is pending, firing ({@code firesInFlight}), or already
     * offered (inbox non-empty / UNPARK-before-PARKED stash) — the
     * predicate can never hold, so the timer must win every time and
     * no {@link DeadlockException} may fire. Directly attacks the
     * §4.2(7) pop-then-fire race: without the counter, the drainer
     * would observe {@code pendingCount == 0} between pop and offer.
     */
    public void testTimerFireStormAtBoundary() throws Exception {
        SchedulerCore sched = newScheduler();
        try {
            for (int i = 0; i < 1000; i++) {
                CountDownLatch resumed = new CountDownLatch(1);
                sched.spawn(() -> {
                    GoroutineHandle self = sched.currentGoroutine();
                    sched.timerHeap().addSleep(System.nanoTime() + 1, self);
                    sched.parkCurrent(resumed::countDown, ParkReason.SELECT);
                });
                assertTrue(resumed.await(10, TimeUnit.SECONDS),
                        "iter " + i + ": timer wakeup won");
                // Must return normally — a DeadlockException here is a
                // false positive (the timer was always in flight).
                sched.awaitQuiescence();
            }
            assertFalse(sched.isDeadlocked(), "no false deadlock across the storm");
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- 8: negative — panic precedence ----------

    /**
     * A recorded panic suppresses the deadlock report: with the panicker
     * dead and the survivor parked, {@code awaitQuiescence} returns via
     * the panic-no-progress wake (the traceback was already printed),
     * never throwing {@link DeadlockException}.
     */
    public void testPanicTakesPrecedenceOverDeadlock() throws Exception {
        SchedulerCore sched = newScheduler();
        PrintStream origErr = System.err;
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
        try {
            AtomicBoolean release = new AtomicBoolean(false);
            sched.spawn(() -> graalgo.runtime.Panic.throwPanic(
                    graalgo.values.GoString.fromUtf8("boom")));
            GoroutineHandle h = sched.spawn(() -> {
                while (!release.get()) {
                    sched.yieldCurrent();
                }
                sched.parkCurrent(null, ParkReason.SELECT);
            });
            release.set(true);
            sched.awaitQuiescence(); // must return, not throw
            assertTrue(sched.panicked(), "panic recorded");
            assertFalse(sched.isDeadlocked(), "no deadlock report on panic precedence");
            assertTrue(errBuf.toString(StandardCharsets.UTF_8).contains("panic: boom"),
                    "traceback printed, stderr was:\n" + errBuf.toString(StandardCharsets.UTF_8));
            sched.unpark(h);
            sched.awaitQuiescence();
        } finally {
            System.setErr(origErr);
            SchedulerCore.install(null);
        }
    }

    // ---------- 9: C1 orphan — admitted after uninstall dies quietly ----------

    /**
     * RV-P1-19 (C1 orphan race): a goroutine spawned but not yet admitted
     * when the launcher uninstalls its scheduler (main returned with
     * children in flight) must die quietly on first admission — never run
     * guest code against a dead or foreign scheduler (its PARKED token
     * would corrupt that scheduler's in-flight accounting, tripping the
     * drainer's corruption halt). Deterministic: the drainer is started
     * only after the uninstall, so the orphan window is staged, not
     * raced.
     */
    public void testOrphanAdmittedAfterUninstallDiesQuietly() throws Exception {
        SchedulerCore schedA = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore prev = SchedulerCore.install(schedA);
        AtomicBoolean ran = new AtomicBoolean(false);
        try {
            // Spawn while the drainer is down: the vthread blocks in
            // awaitAck (first admission), never reaching guest code.
            schedA.spawn(() -> ran.set(true));
            // The launcher exits (C1) and uninstalls the scheduler
            // before the first admission.
            SchedulerCore.install(null);
            // Now start the drainer: INIT is routed, the orphan is
            // admitted, sees its scheduler gone, and dies quietly.
            schedA.start();
            waitFor(() -> schedA.runnableCount() == 0,
                    "orphan released its live count");
            // The admission slot must be balanced too: after the EXIT is
            // routed, the parked map is empty and an admission happened.
            waitFor(() -> schedA.drainer().parkedCountForTest() == 0
                            && schedA.drainer().admissions() >= 1,
                    "orphan EXIT routed");
            assertFalse(ran.get(), "orphan body never ran");
            assertFalse(schedA.crashed(), "no internal-error crash recorded");
            assertFalse(schedA.panicked(), "no panic recorded");
            assertFalse(schedA.isDeadlocked(), "no deadlock recorded");
        } finally {
            SchedulerCore.install(prev);
        }
    }
}
