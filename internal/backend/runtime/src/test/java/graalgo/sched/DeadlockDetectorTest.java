package graalgo.sched;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.jfr.ParkReason;
import graalgo.runtime.GoChan;
import graalgo.runtime.sync.GoWaitGroup;

/**
 * L31 (P1-28): drainer-side deadlock detector tests (gc
 * {@code checkdead} parity).
 *
 * <p>Red-green discipline: the positive tests (verdict expected) fail
 * against a scheduler with no detector — the verdict latch never fires
 * and the await times out. The negative tests (no verdict expected)
 * fail if the watchdog false-positives on legitimate parks: pending
 * timers (even far-future ones), blocking I/O, normal termination.
 *
 * <p>The verdict's production action halts the VM, so tests install a
 * recording handler via {@code SchedulerCore.setDeadlockHandlerForTest}.
 * The handler may fire more than once (the watchdog re-evaluates every
 * 100 ms while the system stays quiescent; production halts on the
 * first); tests assert at-least-once via the latch. Deadlocked
 * goroutines stay parked on daemon virtual threads and the
 * drainer/timer threads are daemon — no per-test teardown beyond
 * uninstalling the scheduler.
 */
public final class DeadlockDetectorTest {

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

    /** Scheduler + recording verdict handler; caller must {@link #close}. */
    private static final class Fixture {
        final SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        final CountDownLatch verdict = new CountDownLatch(1);
        final AtomicInteger verdicts = new AtomicInteger();

        Fixture() {
            sched.start();
            SchedulerCore.install(sched);
            sched.setDeadlockHandlerForTest(() -> {
                verdicts.incrementAndGet();
                verdict.countDown();
            });
        }

        void close() {
            SchedulerCore.install(null);
        }
    }

    /**
     * {@code select {}} in the only goroutine: parks forever with no
     * timers and no I/O — the watchdog must verdict.
     *
     * <p>Red: no detector → the latch never fires → await times out.
     */
    public void testSelectForeverVerdicts() throws Exception {
        Fixture f = new Fixture();
        try {
            CountDownLatch parked = new CountDownLatch(1);
            f.sched.spawn(() -> {
                parked.countDown();
                f.sched.parkCurrent(null, ParkReason.SELECT);
            });
            assertTrue(parked.await(10, TimeUnit.SECONDS), "goroutine parked");
            assertTrue(f.verdict.await(10, TimeUnit.SECONDS),
                    "deadlock verdict fired for select{}");
            assertTrue(f.verdicts.get() >= 1, "verdict recorded");
        } finally {
            f.close();
        }
    }

    /**
     * Cyclic channel deadlock: A recvs ch1 then sends ch2, B recvs ch2
     * then sends ch1 (unbuffered) — both block on recv forever. Genuine
     * channel-level deadlock; the watchdog must verdict.
     *
     * <p>Red: no detector → the latch never fires → await times out.
     */
    public void testCyclicChannelDeadlockVerdicts() throws Exception {
        Fixture f = new Fixture();
        try {
            GoChan ch1 = new GoChan(0, null);
            GoChan ch2 = new GoChan(0, null);
            f.sched.spawn(() -> {
                ch1.recv();
                ch2.send("a");
            });
            f.sched.spawn(() -> {
                ch2.recv();
                ch1.send("b");
            });
            // Deterministic gate: both receivers enqueued before the
            // verdict can be meaningful (a waiter enqueues before it
            // parks, so both PARKED tokens are then in flight).
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
            while (ch1.recvWaitersCount() != 1 || ch2.recvWaitersCount() != 1) {
                assertTrue(System.nanoTime() < deadline, "both receivers waiting");
                Thread.sleep(5);
            }
            assertTrue(f.verdict.await(10, TimeUnit.SECONDS),
                    "deadlock verdict fired for cyclic channel deadlock");
        } finally {
            f.close();
        }
    }

    /**
     * {@code WaitGroup.Wait} with no {@code Done} coming: the waiter
     * parks forever — the watchdog must verdict (gc: same program is a
     * fatal deadlock).
     *
     * <p>Red: no detector → the latch never fires → await times out.
     */
    public void testWaitGroupNoDoneVerdicts() throws Exception {
        Fixture f = new Fixture();
        try {
            GoWaitGroup wg = new GoWaitGroup();
            wg.add(1);
            CountDownLatch waiting = new CountDownLatch(1);
            f.sched.spawn(() -> {
                waiting.countDown();
                wg.await(); // no Done is ever issued
            });
            assertTrue(waiting.await(10, TimeUnit.SECONDS), "waiter started");
            assertTrue(f.verdict.await(10, TimeUnit.SECONDS),
                    "deadlock verdict fired for WaitGroup with no Done");
        } finally {
            f.close();
        }
    }

    /**
     * A panic in one goroutine followed by a genuine deadlock in the
     * rest: the watchdog must still terminate (production: halt(2))
     * rather than hang in {@code awaitQuiescence} forever.
     */
    public void testPanicThenDeadlockVerdicts() throws Exception {
        Fixture f = new Fixture();
        try {
            CountDownLatch parked = new CountDownLatch(1);
            f.sched.spawn(() -> {
                parked.countDown();
                f.sched.parkCurrent(null, ParkReason.SELECT);
            });
            assertTrue(parked.await(10, TimeUnit.SECONDS), "goroutine parked");
            f.sched.spawn(() -> {
                throw new graalgo.runtime.PanicException("boom", null, "string", null);
            });
            // Gate on the panic being RECORDED before the verdict is
            // meaningful: the watchdog may legitimately verdict the
            // parker-only state before the panicker is admitted (a
            // genuine deadlock at that instant), which would satisfy the
            // latch below without the panic premise holding.
            long panicDeadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
            while (!f.sched.panicked()) {
                assertTrue(System.nanoTime() < panicDeadline, "panic recorded");
                Thread.sleep(5);
            }
            assertTrue(f.verdict.await(10, TimeUnit.SECONDS),
                    "deadlock verdict fired after panic + parked waiter");
            assertTrue(f.sched.panicked(), "panic was recorded");
        } finally {
            f.close();
        }
    }

    /**
     * Legitimate park with a pending sleep timer: gc does NOT deadlock
     * while a timer is pending — no verdict until the timer fires and
     * the waiter wakes and exits cleanly.
     *
     * <p>Red (false-positive detector): the verdict fires during the
     * sleep window.
     */
    public void testSleepSuppressesVerdict() throws Exception {
        Fixture f = new Fixture();
        try {
            CountDownLatch awake = new CountDownLatch(1);
            f.sched.spawn(() -> {
                GoroutineHandle h = f.sched.currentGoroutine();
                f.sched.timerHeap().addSleep(
                        System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(300), h);
                f.sched.parkCurrent(null, ParkReason.TIMER);
                awake.countDown();
            });
            // 600 ms covers the whole sleep plus wakeup: no verdict may
            // fire in this window (gc: pending timer suppresses checkdead).
            assertTrue(!f.verdict.await(600, TimeUnit.MILLISECONDS),
                    "no verdict while a sleep timer is pending");
            assertTrue(awake.await(10, TimeUnit.SECONDS), "sleeper woke");
            f.sched.awaitQuiescence();
            assertEquals(0, f.verdicts.get(), "no verdict after clean wakeup");
        } finally {
            f.close();
        }
    }

    /**
     * gc probe parity: {@code time.AfterFunc(time.Hour, ...)} +
     * {@code select {}} does not deadlock. A far-future pending timer
     * suppresses the verdict even though it can never unblock anyone.
     *
     * <p>Red (false-positive detector): the verdict fires.
     */
    public void testFarFutureTimerSuppressesVerdict() throws Exception {
        Fixture f = new Fixture();
        try {
            AtomicReference<GoroutineHandle> waiterRef = new AtomicReference<>();
            CountDownLatch parked = new CountDownLatch(1);
            f.sched.spawn(() -> {
                GoroutineHandle h = f.sched.currentGoroutine();
                waiterRef.set(h);
                long id = f.sched.timerHeap().addSleep(
                        System.nanoTime() + TimeUnit.HOURS.toNanos(1), h);
                parked.countDown();
                try {
                    f.sched.parkCurrent(null, ParkReason.SELECT);
                } finally {
                    f.sched.timerHeap().cancel(id);
                }
            });
            assertTrue(parked.await(10, TimeUnit.SECONDS), "goroutine parked");
            assertTrue(waiterRef.get() != null, "waiter handle captured");
            // Several watchdog ticks (100 ms) pass with a pending timer:
            // no verdict (gc: any P with timers suppresses checkdead).
            assertTrue(!f.verdict.await(500, TimeUnit.MILLISECONDS),
                    "no verdict with a far-future timer pending");
            assertEquals(0, f.verdicts.get(), "no verdict recorded");
            // Clean up: release the waiter (its finally cancels the
            // timer); the scheduler is left idle.
            f.sched.unpark(waiterRef.get());
            f.sched.awaitQuiescence();
            assertEquals(0, f.verdicts.get(), "no verdict after clean release");
        } finally {
            f.close();
        }
    }

    /**
     * A no-op timer callback that fires and unblocks nobody: no verdict
     * while it is pending (gc parity), then a verdict once it has fired
     * and the system settles — this exercises the watchdog's idle-poll
     * tick, since the callback offers no drainer token.
     *
     * <p>Red: no detector → the latch never fires → await times out.
     */
    public void testNoopCallbackFiresThenVerdicts() throws Exception {
        Fixture f = new Fixture();
        try {
            // Arm the callback BEFORE the waiter parks: otherwise the
            // watchdog legitimately verdicts the genuinely-quiescent
            // pre-arm state and the "no verdict while pending" latch
            // assertion below would see the already-fired latch.
            f.sched.timerHeap().addCallback(
                    System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(150),
                    () -> { /* no-op: unblocks nobody, offers no token */ });
            CountDownLatch parked = new CountDownLatch(1);
            f.sched.spawn(() -> {
                parked.countDown();
                f.sched.parkCurrent(null, ParkReason.SELECT);
            });
            assertTrue(parked.await(10, TimeUnit.SECONDS), "goroutine parked");
            // Still well before the 150 ms deadline: the callback is
            // pending, so no verdict is possible (gc: pending timer
            // suppresses).
            assertTrue(!f.verdict.await(50, TimeUnit.MILLISECONDS),
                    "no verdict while the callback is pending");
            // After the callback fires and settles, the system is
            // genuinely quiescent: the watchdog tick must verdict.
            assertTrue(f.verdict.await(10, TimeUnit.SECONDS),
                    "deadlock verdict fired after the no-op callback settled");
        } finally {
            f.close();
        }
    }

    /**
     * Legitimate park while a peer is in blocking I/O: gc does not
     * deadlock while a goroutine is in a syscall (it keeps its M
     * non-idle) — no verdict until the peer leaves I/O, at which point
     * the remaining parked waiter is genuinely deadlocked and the
     * watchdog must verdict.
     *
     * <p>Red (false-positive detector): the verdict fires while the peer
     * is still in I/O. Red (no detector): the final latch never fires.
     */
    public void testBlockingIoSuppressesVerdict() throws Exception {
        Fixture f = new Fixture();
        try {
            CountDownLatch inIo = new CountDownLatch(1);
            CountDownLatch parked = new CountDownLatch(1);
            // The peer enters blocking I/O BEFORE the waiter parks: any
            // earlier verdict would be for a genuinely-quiescent state
            // and would spuriously satisfy the "no verdict" latch below.
            // While the peer is in ioEnter the inIo guard holds, so no
            // verdict is possible before the waiter even parks.
            f.sched.spawn(() -> {
                f.sched.ioEnter();
                inIo.countDown();
                try {
                    Thread.sleep(400); // the "syscall": slot already released
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                } finally {
                    f.sched.ioExit();
                }
            });
            assertTrue(inIo.await(10, TimeUnit.SECONDS), "peer in blocking I/O");
            f.sched.spawn(() -> {
                parked.countDown();
                f.sched.parkCurrent(null, ParkReason.SELECT);
            });
            assertTrue(parked.await(10, TimeUnit.SECONDS), "waiter parked");
            // 250 ms < 400 ms I/O: the waiter is parked but the peer is in
            // a syscall — no verdict (gc _Gsyscall parity).
            assertTrue(!f.verdict.await(250, TimeUnit.MILLISECONDS),
                    "no verdict while a peer is in blocking I/O");
            // The peer leaves I/O and exits; the waiter remains parked
            // forever — now genuinely deadlocked: the verdict must fire.
            assertTrue(f.verdict.await(10, TimeUnit.SECONDS),
                    "deadlock verdict fired after I/O completed");
        } finally {
            f.close();
        }
    }

    /**
     * Normal termination: the last goroutine exits, {@code
     * awaitQuiescence} returns, and no verdict fires on later watchdog
     * ticks (the live==0 guard).
     *
     * <p>Red (missing guard): the verdict fires after clean exit.
     */
    public void testNormalExitNoVerdict() throws Exception {
        Fixture f = new Fixture();
        try {
            f.sched.spawn(() -> { /* trivial work */ });
            f.sched.awaitQuiescence();
            assertEquals(0, f.sched.numGoroutines(), "none live after quiescence");
            // Several watchdog ticks pass with nobody live: no verdict.
            assertTrue(!f.verdict.await(350, TimeUnit.MILLISECONDS),
                    "no verdict after normal termination");
            assertEquals(0, f.verdicts.get(), "no verdict recorded");
        } finally {
            f.close();
        }
    }
}
