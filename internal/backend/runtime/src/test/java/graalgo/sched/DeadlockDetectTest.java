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
import java.util.concurrent.atomic.AtomicReference;

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
 * predicate clause is dropped — except clauses (2) ({@code inflight==0})
 * and (3) ({@code inIo} empty), which are cheap fast-paths subsumed by
 * clause (6): {@code inflight > 0} implies some live handle is not in
 * the parked map, and the parked/inIo maps are disjoint by construction
 * (route throws otherwise), so in either case {@code parked.size() < live}
 * and clause (6) already rejects. They are kept as documented
 * short-circuits, not independently falsifiable clauses.
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
            assertTrue(sched.timerHeap().pendingCount() > 0, "timer was pending");
            // The timer stays armed through teardown: cancelling it before
            // the unparks would open a race — if the first unparked
            // goroutine exits before the second unpark is issued, the
            // detector correctly fires on the still-parked survivor.
            sched.unpark(h1);
            sched.unpark(h2);
            sched.awaitQuiescence();
            assertTrue(sched.timerHeap().cancel(timerId.get()), "timer was pending");
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
     * suppresses an otherwise-true predicate (design §4.2(4)). Production
     * {@code spawn()} does not hold the counter — it increments
     * {@code liveGoroutines} before {@code vthread.start()}, making the
     * newcomer visible in {@code live} throughout the start→INIT-offer
     * window (a spawner-held hold would suppress a fast exiter's EXIT
     * evaluation). The counter is held here via the test-only adjuster to
     * simulate the foreign-spawn window and prove the predicate honors it.
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

    // ---------- 10: C1 orphan — running at uninstall dies quietly ----------

    /**
     * RV-P1-19 (C1 orphan race, post-admission): a goroutine admitted and
     * running guest code when the launcher uninstalls its scheduler (main
     * returned with children in flight) must die quietly when it next
     * touches the scheduler — never a loud "fatal internal error" (in a
     * real process the JVM is exiting anyway; in-process hosts would see
     * only stderr pollution). Deterministic: the orphan spins on a flag
     * until the test thread has uninstalled, then touches the scheduler.
     *
     * <p>Red-green: without the {@code isOrphaned} guard in
     * {@code recordCrash}, the {@code IllegalStateException} from the
     * uninstalled scheduler is reported loudly and the stderr assertion
     * fails.
     */
    public void testOrphanRunningAtUninstallDiesQuietly() throws Exception {
        SchedulerCore schedA = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore prev = SchedulerCore.install(schedA);
        PrintStream origErr = System.err;
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
        CountDownLatch admitted = new CountDownLatch(1);
        AtomicBoolean release = new AtomicBoolean(false);
        AtomicBoolean schedulerTouched = new AtomicBoolean(false);
        try {
            schedA.start();
            schedA.drainer().setAdmissionListener(h -> admitted.countDown());
            schedA.spawn(() -> {
                // Scheduler-aware wait (never JVM-block inside a goroutine):
                // spin until the test thread has uninstalled the scheduler.
                while (!release.get()) {
                    schedA.yieldCurrent();
                }
                // Now touch the scheduler: it is uninstalled, so this
                // throws IllegalStateException("no SchedulerCore installed").
                schedulerTouched.set(true);
                SchedulerCore.current();
            });
            // Wait until the orphan is past the pre-admission guard and
            // running guest code.
            assertTrue(admitted.await(15, TimeUnit.SECONDS), "orphan admitted");
            // The launcher exits (C1) and uninstalls the scheduler while
            // the orphan is still running.
            SchedulerCore.install(null);
            release.set(true);
            waitFor(() -> schedA.runnableCount() == 0,
                    "orphan released its live count");
            assertTrue(schedulerTouched.get(), "orphan touched the scheduler");
            String err = errBuf.toString(StandardCharsets.UTF_8);
            assertFalse(err.contains("fatal internal error"),
                    "orphan died quietly, stderr was:\n" + err);
            assertFalse(schedA.crashed(), "no internal-error crash recorded");
        } finally {
            System.setErr(origErr);
            SchedulerCore.install(prev);
        }
    }

    // ---------- 11: C1 orphan — foreign scheduler install fails fast ----------

    /**
     * RV-P1-19 (C1 orphan, foreign scheduler): if a new scheduler is
     * installed while a previous scheduler's orphan still runs (in-process
     * hosts, sequential launcher runs), the orphan's ambient
     * {@code SchedulerCore.current()} must fail fast instead of returning
     * the foreign scheduler — otherwise the orphan's tokens would corrupt
     * the foreign scheduler's accounting (phantom PARKED → negative
     * inflight → drainer corruption halt). The fail-fast throw is caught
     * by the vthread wrapper and dies quietly via
     * {@code recordCrash}/{@code isOrphaned}.
     *
     * <p>Red-green: without the foreign check in {@code current()}, the
     * orphan gets the foreign scheduler (no throw) and the first
     * assertion fails.
     */
    public void testOrphanAgainstForeignSchedulerFailsFast() throws Exception {
        SchedulerCore schedA = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore prev = SchedulerCore.install(schedA);
        PrintStream origErr = System.err;
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
        CountDownLatch admitted = new CountDownLatch(1);
        AtomicBoolean release = new AtomicBoolean(false);
        AtomicReference<Throwable> observedThrow = new AtomicReference<>();
        AtomicReference<SchedulerCore> gotScheduler = new AtomicReference<>();
        SchedulerCore schedB = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        try {
            schedA.start();
            schedA.drainer().setAdmissionListener(h -> admitted.countDown());
            schedA.spawn(() -> {
                // Scheduler-aware wait (never JVM-block inside a goroutine):
                // spin until the test thread has installed the foreign scheduler.
                while (!release.get()) {
                    schedA.yieldCurrent();
                }
                try {
                    gotScheduler.set(SchedulerCore.current());
                } catch (IllegalStateException ise) {
                    // Fail-fast: rethrow so the vthread wrapper dies quietly.
                    observedThrow.set(ise);
                    throw ise;
                }
            });
            // Wait until the orphan is past the pre-admission guard and
            // running guest code.
            assertTrue(admitted.await(15, TimeUnit.SECONDS), "orphan admitted");
            // A foreign scheduler is installed while the orphan still runs.
            schedB.start();
            SchedulerCore.install(schedB);
            release.set(true);
            waitFor(() -> schedA.runnableCount() == 0,
                    "orphan released its live count");
            // The orphan must NOT have received the foreign scheduler.
            assertTrue(observedThrow.get() != null,
                    "current() failed fast on the foreign scheduler");
            assertTrue(gotScheduler.get() == null,
                    "orphan never ran against the foreign scheduler");
            String err = errBuf.toString(StandardCharsets.UTF_8);
            assertFalse(err.contains("fatal internal error"),
                    "orphan died quietly, stderr was:\n" + err);
            assertFalse(schedA.crashed(), "no internal-error crash recorded");
            // The foreign scheduler is uncorrupted: it runs goroutines normally.
            AtomicBoolean bRan = new AtomicBoolean(false);
            schedB.spawn(() -> bRan.set(true));
            waitFor(() -> schedB.runnableCount() == 0,
                    "foreign scheduler quiesced");
            assertTrue(bRan.get(), "foreign scheduler runs goroutines");
            assertFalse(schedB.crashed(), "foreign scheduler not crashed");
        } finally {
            System.setErr(origErr);
            SchedulerCore.install(prev);
        }
    }

    // ---------- 12: suppressor (predicate clause 5) blocks the report ----------

    /**
     * RV-P1-19 (predicate clause 5): while an external deadlock suppressor
     * is installed — the gc p6 netpoll quirk, wired by
     * {@code GoNetpoll.pollServerInit} once its poller thread exists — an
     * all-parked, timer-less scheduler must NOT report deadlock. Clearing
     * the suppressor re-arms the detector.
     *
     * <p>Red-green: without clause (5), the first park fires and the
     * {@code isDeadlocked} assertion fails.
     */
    public void testSuppressorBlocksDeadlockReport() throws Exception {
        SchedulerCore sched = newScheduler();
        try {
            sched.setDeadlockSuppressor(() -> true);
            GoroutineHandle h = sched.spawn(
                    () -> sched.parkCurrent(null, ParkReason.SELECT));
            // The park has been routed, so the detector has evaluated.
            waitFor(() -> sched.drainer().parkedCountForTest() == 1,
                    "goroutine parked");
            assertFalse(sched.isDeadlocked(), "suppressed: no deadlock report");
            sched.unpark(h);
            sched.awaitQuiescence();
            assertFalse(sched.isDeadlocked(), "still no deadlock report");
            // Clearing the suppressor re-arms the detector.
            sched.setDeadlockSuppressor(null);
            sched.spawn(() -> sched.parkCurrent(null, ParkReason.SELECT));
            try {
                sched.awaitQuiescence();
                throw new AssertionError("expected DeadlockException after suppressor cleared");
            } catch (DeadlockException expected) {
            }
        } finally {
            SchedulerCore.install(null);
        }
    }
}
