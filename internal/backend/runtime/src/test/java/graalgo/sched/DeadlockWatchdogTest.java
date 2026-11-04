package graalgo.sched;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.jfr.ParkReason;
import graalgo.runtime.GoChan;
import graalgo.runtime.Timers;

/**
 * Deadlock watchdog tests (L09, P2-M10).
 *
 * <p>Each test owns a fresh scheduler with the watchdog enabled at
 * short tuning (20&nbsp;ms poll, 3 stable polls, unless noted), so a
 * genuine deadlock is reported in ~60&nbsp;ms while transient states
 * get a wide margin to resolve. Every test cleans up its goroutines
 * (unpark / channel close) so no parked goroutine survives the test.
 *
 * <p>Red-green discipline: without the watchdog these tests fail —
 * the deadlocked cases would hang in {@code awaitQuiescenceOrDeadlock}
 * (the tests bound every wait, so a missing verdict fails instead of
 * hanging the suite) and the must-not-fatal cases would fail on a
 * spurious verdict.
 */
public final class DeadlockWatchdogTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertFalse(boolean cond, String msg) {
        if (cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Polls until the watchdog fires, or fails after {@code timeoutMs}. */
    private static void waitForDeadlock(SchedulerCore sched, long timeoutMs) throws Exception {
        long deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs);
        while (!sched.deadlockDetected()) {
            if (System.nanoTime() > deadline) {
                throw new AssertionError("deadlock watchdog did not fire within "
                        + timeoutMs + "ms");
            }
            Thread.sleep(10);
        }
    }

    /** Polls until {@code cond} holds, or fails after {@code timeoutMs}. */
    private static void waitFor(String what, java.util.concurrent.Callable<Boolean> cond,
                                long timeoutMs) throws Exception {
        long deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs);
        while (!cond.call()) {
            if (System.nanoTime() > deadline) {
                throw new AssertionError("timed out waiting for: " + what);
            }
            Thread.sleep(5);
        }
    }

    private static SchedulerCore newSched() {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        return sched;
    }

    /**
     * A goroutine blocked forever on an empty unbuffered channel recv
     * must fatal: gc's "all goroutines are asleep - deadlock!".
     */
    public void testDeadlockedChannelRecvFatals() throws Exception {
        SchedulerCore sched = newSched();
        GoChan ch = new GoChan(0, null);
        GoroutineHandle h = null;
        try {
            sched.enableDeadlockWatchdog(20, 3);
            h = sched.spawn(() -> ch.recv());
            waitFor("receiver parked", () -> ch.recvWaitersCount() == 1, 5000);
            waitForDeadlock(sched, 5000);
            assertTrue(sched.deadlockReport() != null
                    && sched.deadlockReport().contains("all goroutines are asleep - deadlock!"),
                    "report carries the gc fatal line");
            assertTrue(sched.deadlockReport().contains("CHANNEL"),
                    "report names the parked goroutine's reason: " + sched.deadlockReport());
            assertFalse(sched.awaitQuiescenceOrDeadlock(),
                    "awaitQuiescenceOrDeadlock reports deadlock, not quiescence");
        } finally {
            ch.close();
            if (h != null) {
                sched.unpark(h);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }

    /**
     * A goroutine parked in an empty select (SELECT reason, the
     * SelectOp.java case) must fatal the same way.
     */
    public void testDeadlockedEmptySelectFatals() throws Exception {
        SchedulerCore sched = newSched();
        GoroutineHandle h = null;
        try {
            sched.enableDeadlockWatchdog(20, 3);
            CountDownLatch aboutToPark = new CountDownLatch(1);
            h = sched.spawn(() -> {
                aboutToPark.countDown();
                sched.parkCurrent(null, ParkReason.SELECT);
            });
            assertTrue(aboutToPark.await(5, TimeUnit.SECONDS), "goroutine started");
            waitForDeadlock(sched, 5000);
            assertTrue(sched.deadlockReport().contains("SELECT"),
                    "report names the SELECT park: " + sched.deadlockReport());
        } finally {
            if (h != null) {
                sched.unpark(h);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }

    /**
     * Legitimately long-parked must NOT fatal: the receiver parks on a
     * channel while the sender sleeps on a timer (a pending wake
     * source) and then sends. The program completes normally.
     */
    public void testLongParkedWithTimerWakeDoesNotFatal() throws Exception {
        SchedulerCore sched = newSched();
        GoChan ch = new GoChan(0, null);
        AtomicReference<Object> got = new AtomicReference<>();
        GoroutineHandle recvH = null;
        GoroutineHandle sendH = null;
        try {
            sched.enableDeadlockWatchdog(20, 3);
            recvH = sched.spawn(() -> got.set(ch.recv().value()));
            sendH = sched.spawn(() -> {
                Timers.sleep(300_000_000L);
                ch.send("done");
            });
            assertTrue(sched.awaitQuiescenceOrDeadlock(),
                    "timer-woken program reaches quiescence, not deadlock");
            assertFalse(sched.deadlockDetected(), "no spurious verdict");
            assertEquals("done", got.get(), "receiver got the sender's value");
        } finally {
            // Failure-path cleanup only (on success both goroutines
            // already exited): wake the receiver, then close so a
            // late sender dies by panic instead of parking forever.
            if (recvH != null) {
                sched.unpark(recvH);
            }
            try {
                ch.close();
            } catch (RuntimeException e) {
                // Already closed on some failure paths; ignore.
            }
            if (sendH != null) {
                sched.unpark(sendH);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }

    /**
     * A pending timer blocks the verdict, but only while pending: a
     * no-op callback timer fires (waking nobody) and the watchdog must
     * stay silent before the fire and fatal after it.
     */
    public void testPendingTimerBlocksVerdictUntilItFires() throws Exception {
        SchedulerCore sched = newSched();
        GoroutineHandle h = null;
        try {
            sched.enableDeadlockWatchdog(20, 3);
            CountDownLatch aboutToPark = new CountDownLatch(1);
            h = sched.spawn(() -> {
                aboutToPark.countDown();
                sched.parkCurrent(null, ParkReason.CHANNEL);
            });
            assertTrue(aboutToPark.await(5, TimeUnit.SECONDS), "goroutine started");
            sched.timerHeap().addCallback(
                    System.nanoTime() + 200_000_000L, () -> { });
            Thread.sleep(60);
            assertFalse(sched.deadlockDetected(),
                    "pending timer must block the verdict");
            waitForDeadlock(sched, 5000);
        } finally {
            if (h != null) {
                sched.unpark(h);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }

    /**
     * A goroutine in blocking I/O blocks the verdict while the syscall
     * is outstanding; once it exits, the remaining parked goroutine
     * deadlocks and must fatal.
     */
    public void testBlockingIoBlocksVerdictThenFatals() throws Exception {
        SchedulerCore sched = newSched();
        GoroutineHandle parked = null;
        try {
            sched.enableDeadlockWatchdog(20, 3);
            sched.spawn(() -> {
                sched.ioEnter();
                try {
                    Thread.sleep(400);
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                } finally {
                    sched.ioExit();
                }
            });
            CountDownLatch aboutToPark = new CountDownLatch(1);
            parked = sched.spawn(() -> {
                aboutToPark.countDown();
                sched.parkCurrent(null, ParkReason.CHANNEL);
            });
            assertTrue(aboutToPark.await(5, TimeUnit.SECONDS), "goroutine started");
            Thread.sleep(150);
            assertFalse(sched.deadlockDetected(),
                    "outstanding blocking I/O must block the verdict");
            waitForDeadlock(sched, 5000);
        } finally {
            if (parked != null) {
                sched.unpark(parked);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }

    /**
     * A transient all-parked state that resolves before the stability
     * window elapses must NOT fatal (guards drainer transits and slow
     * wakeup paths against false positives).
     */
    public void testTransientAllParkedThenWakeDoesNotFatal() throws Exception {
        SchedulerCore sched = newSched();
        GoroutineHandle h = null;
        try {
            // 50ms poll x 3 stable polls = 150ms window; the wakeup
            // lands at ~60ms, inside the window.
            sched.enableDeadlockWatchdog(50, 3);
            CountDownLatch aboutToPark = new CountDownLatch(1);
            h = sched.spawn(() -> {
                aboutToPark.countDown();
                sched.parkCurrent(null, ParkReason.CHANNEL);
            });
            assertTrue(aboutToPark.await(5, TimeUnit.SECONDS), "goroutine started");
            Thread.sleep(60);
            sched.unpark(h);
            assertTrue(sched.awaitQuiescenceOrDeadlock(),
                    "woken goroutine reaches quiescence");
            Thread.sleep(300);
            assertFalse(sched.deadlockDetected(),
                    "transient park must not produce a verdict");
        } finally {
            if (h != null) {
                sched.unpark(h);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }

    /**
     * A registered external wake source (the netpoll hook's contract)
     * blocks the verdict; clearing it unblocks the verdict.
     */
    public void testExternalWakeSourceBlocksVerdict() throws Exception {
        SchedulerCore sched = newSched();
        GoroutineHandle h = null;
        java.util.concurrent.atomic.AtomicInteger ext =
                new java.util.concurrent.atomic.AtomicInteger(1);
        try {
            sched.setExternalWakeSourceCount(ext::get);
            sched.enableDeadlockWatchdog(20, 3);
            CountDownLatch aboutToPark = new CountDownLatch(1);
            h = sched.spawn(() -> {
                aboutToPark.countDown();
                sched.parkCurrent(null, ParkReason.NETPOLL);
            });
            assertTrue(aboutToPark.await(5, TimeUnit.SECONDS), "goroutine started");
            Thread.sleep(200);
            assertFalse(sched.deadlockDetected(),
                    "external wake source must block the verdict");
            ext.set(0);
            waitForDeadlock(sched, 5000);
        } finally {
            if (h != null) {
                sched.unpark(h);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }

    /**
     * The watchdog is off by default: a parked-everything scheduler
     * without enableDeadlockWatchdog() never reports a verdict.
     */
    public void testWatchdogDisabledByDefault() throws Exception {
        SchedulerCore sched = newSched();
        GoroutineHandle h = null;
        try {
            CountDownLatch aboutToPark = new CountDownLatch(1);
            h = sched.spawn(() -> {
                aboutToPark.countDown();
                sched.parkCurrent(null, ParkReason.CHANNEL);
            });
            assertTrue(aboutToPark.await(5, TimeUnit.SECONDS), "goroutine started");
            Thread.sleep(200);
            assertFalse(sched.deadlockDetected(), "disabled watchdog stays silent");
        } finally {
            if (h != null) {
                sched.unpark(h);
            }
            sched.awaitQuiescence();
            SchedulerCore.install(null);
        }
    }
}
