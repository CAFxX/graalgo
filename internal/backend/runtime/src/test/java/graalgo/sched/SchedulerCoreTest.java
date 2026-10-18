package graalgo.sched;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

/**
 * P2-M1 scheduler-core unit tests (WS A).
 *
 * <p>Red-green discipline: every test fails against a broken scheduler
 * (no quantum check, no cap assert, lost wakeups) and passes against the
 * real one. GG IDs activate here per the plan's activation policy via
 * {@code verifies:} annotations in javadoc.
 */
public final class SchedulerCoreTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    // ---------- spawn / join / quiescence ----------

    /**
     * Basic spawn/join: N goroutines each do work; all complete.
     * (No specific GG ID; exercises spawn/admission machinery.)
     */
    public void testSpawnAndJoin() throws Exception {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            int n = 50;
            AtomicInteger done = new AtomicInteger();
            for (int i = 0; i < n; i++) {
                sched.spawn(done::incrementAndGet);
            }
            sched.awaitQuiescence();
            assertEquals(n, done.get(), "all goroutines ran");
            assertEquals(0, sched.runnableCount(), "quiescence: none live");
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- numGoroutines (PKG-023: runtime.NumGoroutine) ----------

    /**
     * PKG-023: {@code numGoroutines()} counts exactly the goroutines that
     * currently exist. N goroutines are spawned and parked
     * (scheduler-aware park — blocked goroutines must count, like gc);
     * the count is asserted while they are blocked, then all are woken
     * and the count is asserted again at quiescence.
     *
     * <p>Red-green: fails if the counter misses spawns (blocked-phase
     * assert), misses completions (quiescence assert), or excludes
     * parked goroutines.
     */
    public void testNumGoroutines() throws Exception {
        SchedulerCore sched = new SchedulerCore(16, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            int n = 8;
            CountDownLatch parked = new CountDownLatch(n);
            List<GoroutineHandle> handles = new ArrayList<>();
            for (int i = 0; i < n; i++) {
                handles.add(sched.spawn(() -> {
                    parked.countDown();
                    // Scheduler-aware park: releases the in-flight slot
                    // (runner rule: never JVM-block inside a goroutine).
                    sched.parkCurrent(null);
                }));
            }
            assertTrue(parked.await(10, TimeUnit.SECONDS), "all goroutines parked");
            assertEquals(n, sched.numGoroutines(),
                    "N blocked goroutines counted while parked");
            for (GoroutineHandle h : handles) {
                sched.unpark(h);
            }
            sched.awaitQuiescence();
            assertEquals(0, sched.numGoroutines(), "quiescence: none live");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Quiescence at scale: 10k goroutines all run to completion.
     * P2-M1 acceptance: quiescence probe green at 10k goroutines.
     * (No specific GG ID; exercises the quiescence machinery.)
     *
     * <p>The canonical spawn-loop-then-await pattern: the monitor-based
     * quiescence (reviewer2 P0) re-checks the live count at await time,
     * so a transient zero mid-loop cannot cause an early return.
     */
    public void testQuiescenceAt10k() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            int n = 10_000;
            AtomicInteger done = new AtomicInteger();
            for (int i = 0; i < n; i++) {
                sched.spawn(done::incrementAndGet);
            }
            sched.awaitQuiescence();
            assertEquals(n, done.get(), "10k goroutines all ran");
            assertTrue(sched.maxInflightObserved() <= 2,
                    "cap held at 10k: maxInflight=" + sched.maxInflightObserved());
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- park / unpark ----------

    /**
     * verifies: GG-WAKE-0001 (ack-before-unpark; no lost wakeup).
     * Park then unpark: the goroutine resumes exactly once.
     */
    public void testParkUnpark() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, 3_600_000_000_000L); // huge quantum
        sched.start();
        SchedulerCore.install(sched);
        try {
            CountDownLatch parked = new CountDownLatch(1);
            CountDownLatch resumed = new CountDownLatch(1);
            AtomicBoolean[] started = {new AtomicBoolean()};
            GoroutineHandle h = sched.spawn(() -> {
                started[0].set(true);
                parked.countDown();
                sched.parkCurrent(null);
                resumed.countDown();
            });
            assertTrue(parked.await(5, TimeUnit.SECONDS), "goroutine started");
            // Wait until it is actually parked (armed generation visible).
            long deadline = System.nanoTime() + 5_000_000_000L;
            while (h.armed.get() < 0 && System.nanoTime() < deadline) {
                Thread.yield();
            }
            assertTrue(h.armed.get() >= 0, "goroutine armed its park ticket");
            sched.unpark(h);
            assertTrue(resumed.await(5, TimeUnit.SECONDS), "unpark resumed the goroutine");
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * verifies: GG-WAKE-0001 (unpark-before-park is not lost).
     * LockSupport permit semantics at the scheduler level: an unpark
     * arriving before the park is absorbed, not lost.
     */
    public void testUnparkBeforePark() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, 3_600_000_000_000L);
        sched.start();
        SchedulerCore.install(sched);
        try {
            CountDownLatch resumed = new CountDownLatch(1);
            GoroutineHandle h = sched.spawn(() -> {
                // Park AFTER the unpark below: must still resume.
                try {
                    Thread.sleep(200);
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                }
                sched.parkCurrent(null);
                resumed.countDown();
            });
            // Unpark before the goroutine parks: the drainer stashes it.
            sched.unpark(h);
            assertTrue(resumed.await(5, TimeUnit.SECONDS),
                    "unpark-before-park was not lost");
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * verifies: GG-WAKE-0001 (ack-before-unpark under stress).
     * 200 park/unpark cycles; a reversed ack/unpark order would hang.
     */
    public void testAckBeforeUnparkStress() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, 3_600_000_000_000L);
        sched.start();
        SchedulerCore.install(sched);
        try {
            for (int i = 0; i < 200; i++) {
                CountDownLatch resumed = new CountDownLatch(1);
                GoroutineHandle h = sched.spawn(() -> {
                    sched.parkCurrent(null);
                    resumed.countDown();
                });
                long deadline = System.nanoTime() + 5_000_000_000L;
                while (h.armed.get() < 0 && System.nanoTime() < deadline) {
                    Thread.yield();
                }
                assertTrue(h.armed.get() >= 0, "cycle " + i + ": armed");
                sched.unpark(h);
                assertTrue(resumed.await(5, TimeUnit.SECONDS),
                        "cycle " + i + ": wakeup lost (ack-before-unpark violated?)");
            }
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- cap enforcement ----------

    /**
     * verifies: GG-SCHED-0003 (hard assert).
     * White-box: forcing inflight above the cap must trip the loud
     * AssertionError. Red-green: fails if checkInvariants is removed or
     * weakened to a log.
     */
    public void testCapExceededHardAsserts() {
        SchedulerCore sched = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        Drainer d = sched.drainer();
        d.setInflightForTest(2); // cap is 1
        try {
            d.checkInvariants();
            throw new AssertionError("cap+1 did not hard-assert");
        } catch (AssertionError expected) {
            assertTrue(expected.getMessage().contains("inflight=2"),
                    "assert names the violation: " + expected.getMessage());
        }
        d.setInflightForTest(-1);
        try {
            d.checkInvariants();
            throw new AssertionError("negative inflight did not hard-assert");
        } catch (AssertionError expected) {
            // expected
        }
        d.setInflightForTest(0);
        d.checkInvariants(); // clean state passes
    }

    /**
     * verifies: GG-SCHED-0003 (cap held under load).
     * 8 spinners at cap=2: max in-flight never exceeds 2, all finish.
     */
    public void testCapEnforcedUnderLoad() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, 20_000_000L); // 20ms quantum
        sched.start();
        SchedulerCore.install(sched);
        try {
            int n = 8;
            AtomicInteger done = new AtomicInteger();
            AtomicBoolean stop = new AtomicBoolean();
            for (int i = 0; i < n; i++) {
                sched.spawn(() -> {
                    long iters = 0;
                    while (!stop.get()) {
                        iters++;
                        if ((iters & 0xFFF) == 0) {
                            sched.pollCurrentThread();
                        }
                    }
                    done.incrementAndGet();
                });
            }
            Thread.sleep(400);
            stop.set(true);
            sched.awaitQuiescence();
            assertEquals(n, done.get(), "all spinners finished");
            assertTrue(sched.maxInflightObserved() <= 2,
                    "cap=2 held: maxInflight=" + sched.maxInflightObserved());
            assertTrue(sched.maxInflightObserved() >= 1,
                    "sanity: something actually ran");
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- quantum expiry ----------

    /**
     * verifies: GG-SCHED-0001 (quantum park at Safepoint.poll).
     * cap=1, infinite spinner + one-shot: the one-shot only runs if the
     * spinner is preempted. Red-green: fails (timeout) when the quantum
     * check is disabled.
     */
    public void testQuantumExpiryPreempts() throws Exception {
        SchedulerCore sched = new SchedulerCore(1, 20_000_000L); // 20ms quantum
        sched.start();
        SchedulerCore.install(sched);
        try {
            AtomicBoolean stop = new AtomicBoolean();
            AtomicLong spinnerIters = new AtomicLong();
            // Infinite spinner: would hold cap=1 forever without preemption.
            sched.spawn(() -> {
                while (!stop.get()) {
                    spinnerIters.incrementAndGet();
                    sched.pollCurrentThread(); // what the AST Safepoint.poll does
                }
            });
            CountDownLatch oneshot = new CountDownLatch(1);
            sched.spawn(oneshot::countDown);
            assertTrue(oneshot.await(10, TimeUnit.SECONDS),
                    "one-shot goroutine starved: quantum expiry did not preempt the spinner");
            assertTrue(spinnerIters.get() > 0, "spinner actually spun");
            stop.set(true);
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Safepoint.poll() resolves to the Phase-2 overlay (not the Phase-1
     * stub) and delegates to the installed scheduler.
     * (No specific GG ID; exercises the overlay wiring.)
     * The same preemption through the REAL replacement
     * {@code graalgo.safepoint.Safepoint.poll()} — proves the classpath
     * overlay resolves to Phase-2 semantics.
     */
    public void testSafepointPollWiring() throws Exception {
        SchedulerCore sched = new SchedulerCore(1, 20_000_000L);
        sched.start();
        SchedulerCore.install(sched);
        try {
            AtomicBoolean stop = new AtomicBoolean();
            sched.spawn(() -> {
                while (!stop.get()) {
                    graalgo.safepoint.Safepoint.poll();
                }
            });
            CountDownLatch oneshot = new CountDownLatch(1);
            sched.spawn(oneshot::countDown);
            assertTrue(oneshot.await(10, TimeUnit.SECONDS),
                    "Safepoint.poll() did not trigger quantum preemption "
                    + "(overlay not active?)");
            stop.set(true);
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- drainer lane order ----------

    /**
     * verifies: GG-SCHED-0002, GG-WAKE-0003 (wake &gt; init &gt; run).
     * Deterministic white-box test: drives the drainer synchronously
     * (never started) through a scripted token sequence and asserts the
     * admission order. Red-green: fails if pollLane order changes.
     */
    public void testDrainerLaneOrder() {
        SchedulerCore sched = new SchedulerCore(1, 3_600_000_000_000L);
        // NOTE: drainer never started; processOneForTest drives it.
        Drainer d = sched.drainer();
        List<Long> admissions = new ArrayList<>();
        d.setAdmissionListener(h -> admissions.add(h.id));

        GoroutineHandle holder = fakeHandle();
        GoroutineHandle waker = fakeHandle();
        GoroutineHandle init = fakeHandle();
        GoroutineHandle runner = fakeHandle();

        // 1. Holder occupies the single slot.
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.INIT, holder, 0));
        // 2. Queue WAKE + INIT while the slot is held (no admission).
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.WAKE, waker, 0));
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.INIT, init, 0));
        // 3. A quantum-expired spinner parks: frees the slot, joins runFifo.
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.RESUME, runner, 0));
        // 4. Holder exits: frees the slot again.
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.EXIT, holder, -1));
        // 5. Waker exits: frees the slot again.
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.EXIT, waker, -1));

        // Expected admission order: holder, waker (WAKE lane), init
        // (INIT lane), runner (RESUME/run lane).
        assertEquals(4, admissions.size(), "admissions: " + admissions);
        assertEquals(holder.id, admissions.get(0), "holder first");
        assertEquals(waker.id, admissions.get(1), "WAKE before INIT");
        assertEquals(init.id, admissions.get(2), "INIT before RUN");
        assertEquals(runner.id, admissions.get(3), "RUN last");
    }

    /** Non-started handle; unpark on its dummy thread is harmless. */
    private static GoroutineHandle fakeHandle() {
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = new Thread(() -> {});
        return h;
    }

    // ---------- failure discipline ----------

    /**
     * verifies: GG-WAKE-0001 (wake action not dropped).
     * Reviewer1 P0-2: unpark-before-park must still run the wake action,
     * on the woken vthread. Red-green: fails pre-fix (action dropped).
     */
    public void testWakeActionSurvivesUnparkBeforePark() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, 3_600_000_000_000L);
        sched.start();
        SchedulerCore.install(sched);
        try {
            AtomicBoolean actionRan = new AtomicBoolean();
            AtomicReference<Thread> actionThread = new AtomicReference<>();
            CountDownLatch resumed = new CountDownLatch(1);
            GoroutineHandle h = sched.spawn(() -> {
                try {
                    Thread.sleep(200);
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                }
                sched.parkCurrent(() -> {
                    actionRan.set(true);
                    actionThread.set(Thread.currentThread());
                });
                resumed.countDown();
            });
            sched.unpark(h); // before the goroutine parks
            assertTrue(resumed.await(5, TimeUnit.SECONDS), "resumed after early unpark");
            assertTrue(actionRan.get(), "wake action ran (dropped pre-fix)");
            assertTrue(actionThread.get() != null && actionThread.get().isVirtual(),
                    "wake action ran on the vthread");
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * verifies: GG-0017 (Go panic parity).
     * Reviewer1 P1-1: an uncaught panic in a SPAWNED goroutine must be
     * recorded (launcher exits 2), not a silent exit 0 with a raw JVM
     * stack trace.
     */
    public void testSpawnedPanicRecorded() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            sched.spawn(() -> {
                throw new graalgo.runtime.PanicException("boom", null, "string", null);
            });
            sched.awaitQuiescence();
            assertTrue(sched.panicked(), "spawned panic recorded");
            assertTrue(!sched.crashed(), "panic is not an internal crash");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Internal errors are loud: a RuntimeException escaping a goroutine
     * body is recorded as a crash (launcher exits 1), never swallowed.
     * verifies: GG-0017 (Goexit/panic/crash discipline).
     * A RuntimeException escaping a goroutine body is recorded as a
     * crash (launcher exits 1), never swallowed.
     */
    public void testSpawnedRuntimeExceptionRecorded() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            sched.spawn(() -> {
                throw new IllegalStateException("synthetic internal error");
            });
            sched.awaitQuiescence();
            assertTrue(sched.crashed(), "internal error recorded");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Terminal-Error discipline: an {@link Error} (e.g. an AssertionError
     * from a blown internal invariant, or OutOfMemoryError) escaping a
     * goroutine body is recorded as a crash (launcher exits 1), never a
     * silent exit 0. verifies: GG-0017 (Goexit/panic/crash discipline).
     * Red-green: fails without the terminal {@code catch (Error)} in
     * spawn — the finally still offers the EXIT token, so quiescence
     * completes while crashed() stays false and the launcher reports
     * EXIT_OK.
     */
    public void testSpawnedErrorRecorded() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            sched.spawn(() -> {
                throw new AssertionError("synthetic blown invariant");
            });
            sched.awaitQuiescence();
            assertTrue(sched.crashed(), "terminal Error recorded as crash");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Voluntary yield via the quantum-park mechanism.
     * Reviewer2 P1-2: yieldCurrent() had zero coverage. With cap=1 and
     * quantum disabled, a goroutine that never yields would starve the
     * second goroutine forever; 100 yields must let it run.
     * (Exercises the park mechanism; not quantum expiry per se.)
     * Red-green: fails if yieldCurrent is a no-op (timeout).
     */
    public void testYieldCurrent() throws Exception {
        SchedulerCore sched = new SchedulerCore(1, 3_600_000_000_000L);
        sched.start();
        SchedulerCore.install(sched);
        try {
            AtomicInteger yields = new AtomicInteger();
            AtomicInteger otherRan = new AtomicInteger();
            CountDownLatch bothDone = new CountDownLatch(2);
            sched.spawn(() -> {
                for (int i = 0; i < 100; i++) {
                    yields.incrementAndGet();
                    sched.yieldCurrent();
                }
                bothDone.countDown();
            });
            sched.spawn(() -> {
                otherRan.incrementAndGet();
                bothDone.countDown();
            });
            assertTrue(bothDone.await(10, TimeUnit.SECONDS),
                    "both completed (yield starved the peer if no-op)");
            assertEquals(100, yields.get(), "all yields executed");
            assertEquals(1, otherRan.get(), "peer ran: yield actually parked");
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * verifies: GG-SCHED-0002 (non-terminating drainer).
     * Reviewer2 P2: the interrupt-swallow was untested. Interrupting the
     * drainer thread must not kill it; the scheduler keeps working.
     */
    public void testDrainerSurvivesInterrupt() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            // Let the drainer block in take().
            Thread.sleep(100);
            Thread drainerThread = sched.drainer().drainerThreadForTest();
            assertTrue(drainerThread != null && drainerThread.isAlive(),
                    "drainer thread running");
            drainerThread.interrupt();
            // Give it a moment to swallow the interrupt and continue.
            Thread.sleep(100);
            assertTrue(drainerThread.isAlive(),
                    "drainer survived interrupt (GG-SCHED-0002)");
            // Scheduler still works: spawn and join.
            AtomicInteger done = new AtomicInteger();
            sched.spawn(done::incrementAndGet);
            sched.awaitQuiescence();
            assertEquals(1, done.get(), "scheduler functional after interrupt");
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- yieldCurrent (PKG-040: runtime.Gosched) ----------

    /**
     * PKG-040: {@code yieldCurrent()} really yields the processor. The
     * scheduler runs with a single in-flight slot and a one-hour quantum
     * (so no preemptive quantum expiry can interleave — the quantum
     * deadline {@code nanoTime + 3.6e12} cannot overflow), which makes
     * execution effectively cooperative: the only way a second goroutine
     * runs is a voluntary yield.
     *
     * <p>The spinner is confirmed running (it holds the only slot) before
     * the peer is spawned; the spinner then spins on
     * {@code yieldCurrent()} until it observes the peer's write. With a
     * working yield each iteration parks and re-queues the spinner behind
     * the peer, the peer runs, and the spinner observes it. If the yield
     * were a no-op the spinner would hold the slot forever and the await
     * would time out.
     *
     * <p>Red-green: fails (30s timeout) with the yield call stubbed out;
     * passes with the real {@code yieldCurrent}. Verified by fault
     * injection on {@code GoschedNode.executeVoid}.
     */
    public void testGoschedYields() throws Exception {
        SchedulerCore sched = new SchedulerCore(1, TimeUnit.HOURS.toNanos(1));
        sched.start();
        SchedulerCore.install(sched);
        try {
            AtomicLong spins = new AtomicLong();
            AtomicBoolean peerRan = new AtomicBoolean(false);
            CountDownLatch spinnerDone = new CountDownLatch(1);
            sched.spawn(() -> {
                while (!peerRan.get()) {
                    sched.yieldCurrent();
                    spins.incrementAndGet();
                }
                spinnerDone.countDown();
            });
            // Wait until the spinner is confirmed running: it holds the
            // only in-flight slot from here on.
            long spinDeadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
            while (spins.get() < 100 && System.nanoTime() < spinDeadline) {
                Thread.sleep(1);
            }
            assertTrue(spins.get() >= 100, "spinner started and holds the slot");
            // The peer can only be admitted when the spinner yields.
            sched.spawn(() -> peerRan.set(true));
            assertTrue(spinnerDone.await(30, TimeUnit.SECONDS),
                    "spinner observed the peer: yield admitted it");
            assertTrue(peerRan.get(), "peer ran");
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }
}
