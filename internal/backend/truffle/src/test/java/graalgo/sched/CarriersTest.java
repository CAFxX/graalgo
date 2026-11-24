package graalgo.sched;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Carrier mode (ADR-0018): goroutines as continuations on a few carrier threads.
 *
 * <p>The structural property behind O(1) spawn — no thread per goroutine — is asserted
 * directly (the carrier count stays at the pool target however many goroutines live); the
 * park/unpark permit protocol, the pinned-park fallback and blocking compensation each have a
 * test that fails when the mechanism is removed. Carrier mode is the default (GG-0073), so the
 * scheduler's own suites (LifecycleTest, ChannelTest, WakeupRaceStressTest) run in it; {@code
 * make test-goroutine-modes} reruns them in the thread-per-goroutine fallback.
 *
 * <p>verifies: GG-SCHED-0003 (carrier slice: in-flight cap with carriers), GG-WAKE-0001
 * (carrier slice: no lost wakeup through the permit), GG-SCHED-0006 (carrier slice: a
 * goroutine blocked in a system call holds a carrier, a spare replaces it)
 */
public final class CarriersTest {
    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static SchedulerCore carrierScheduler(int gomaxprocs) {
        SchedulerCore s = new SchedulerCore(gomaxprocs, SchedulerCore.DEFAULT_QUANTUM_NANOS,
                r -> Thread.ofPlatform().unstarted(r), null, true);
        assertTrue(s.carrierMode(), "carrier mode");
        s.start();
        return s;
    }

    private static void awaitTrue(java.util.function.BooleanSupplier c, String what) throws InterruptedException {
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20);
        while (!c.getAsBoolean()) {
            if (System.nanoTime() > deadline) {
                throw new AssertionError("timed out: " + what);
            }
            Thread.sleep(1);
        }
    }

    /**
     * 5,000 goroutines parked at once run on the carrier pool: the number of carrier threads
     * never exceeds the pool target (thread mode would hold 5,000 threads). Mutation: a thread
     * per goroutine — red.
     */
    public void testParkedGoroutinesHoldNoThread() throws Exception {
        SchedulerCore s = carrierScheduler(2);
        SchedulerCore prev = SchedulerCore.install(s);
        try {
            int n = 5_000;
            CountDownLatch started = new CountDownLatch(n);
            List<GoroutineHandle> hs = new ArrayList<>(n);
            for (int i = 0; i < n; i++) {
                hs.add(s.spawn(() -> {
                    started.countDown();
                    s.parkCurrent(null);
                }));
            }
            assertTrue(started.await(20, TimeUnit.SECONDS), "all goroutines started");
            awaitTrue(() -> s.numGoroutines() == n, "live count");
            int carriers = s.carriersForTest().aliveCarriers();
            assertTrue(carriers <= s.carrierParallelism(),
                    "carriers " + carriers + " > pool target " + s.carrierParallelism());
            for (GoroutineHandle h : hs) {
                s.unpark(h);
            }
            s.awaitQuiescence();
            assertTrue(s.maxInflightObserved() <= 2, "in-flight cap held: " + s.maxInflightObserved());
        } finally {
            SchedulerCore.install(prev);
            assertTrue(s.terminate(10_000).isEmpty(), "carriers stopped");
        }
    }

    /**
     * A park that cannot yield (the continuation is pinned) blocks its carrier; a spare carrier
     * runs the goroutine that wakes it. One pinned goroutine per carrier of the pool: without the
     * spare (mutation: the pool does not count blocked carriers) the waker never runs — red by
     * timeout (checked).
     */
    public void testPinnedParkGetsSpareCarrier() throws Exception {
        SchedulerCore s = carrierScheduler(64);
        SchedulerCore prev = SchedulerCore.install(s);
        try {
            int pinned = s.carrierParallelism();
            CountDownLatch parked = new CountDownLatch(pinned);
            List<GoroutineHandle> hs = new ArrayList<>();
            AtomicInteger resumed = new AtomicInteger();
            for (int i = 0; i < pinned; i++) {
                hs.add(s.spawn(() -> {
                    Carriers.pinCurrent();
                    try {
                        parked.countDown();
                        s.parkCurrent(null);
                        resumed.incrementAndGet();
                    } finally {
                        Carriers.unpinCurrent();
                    }
                }));
            }
            CountDownLatch woke = new CountDownLatch(1);
            s.spawn(() -> {
                try {
                    parked.await();
                } catch (InterruptedException e) {
                    throw new IllegalStateException(e);
                }
                for (GoroutineHandle h : hs) {
                    s.unpark(h);
                }
                woke.countDown();
            });
            assertTrue(woke.await(20, TimeUnit.SECONDS), "waker ran on a spare carrier");
            s.awaitQuiescence();
            assertTrue(resumed.get() == pinned, "pinned goroutines resumed: " + resumed.get());
            assertTrue(s.carriersForTest().pinnedParks.get() >= 1, "parks were pinned");
        } finally {
            SchedulerCore.install(prev);
            assertTrue(s.terminate(10_000).isEmpty(), "carriers stopped");
        }
    }

    /**
     * Goroutines blocked in a system call ({@code ioEnter}…{@code ioExit}) each hold their
     * carrier; spares keep the pool able to run the goroutine that releases them. Mutation: the pool
     * does not count blocked carriers — red by timeout (checked).
     */
    public void testBlockingCallsGetSpareCarriers() throws Exception {
        SchedulerCore s = carrierScheduler(64);
        SchedulerCore prev = SchedulerCore.install(s);
        try {
            int blockers = s.carrierParallelism() + 1;
            CountDownLatch release = new CountDownLatch(1);
            CountDownLatch inIo = new CountDownLatch(blockers);
            for (int i = 0; i < blockers; i++) {
                s.spawn(() -> {
                    s.ioEnter();
                    try {
                        inIo.countDown();
                        release.await(); // blocks the carrier, like a system call
                    } catch (InterruptedException e) {
                        throw new IllegalStateException(e);
                    } finally {
                        s.ioExit();
                    }
                });
            }
            assertTrue(inIo.await(20, TimeUnit.SECONDS), "all blockers in I/O");
            s.spawn(release::countDown);
            s.awaitQuiescence();
        } finally {
            SchedulerCore.install(prev);
            assertTrue(s.terminate(10_000).isEmpty(), "carriers stopped");
        }
    }

    /**
     * Teardown unwinds every live goroutine — parked, never admitted, frozen by exit — and stops
     * the carriers. Mutation: terminate without unparking — the live count stays, red.
     */
    public void testTerminateUnwindsAndStopsCarriers() throws Exception {
        SchedulerCore s = carrierScheduler(1);
        SchedulerCore prev = SchedulerCore.install(s);
        AtomicInteger unwound = new AtomicInteger();
        AtomicInteger parking = new AtomicInteger();
        try {
            for (int i = 0; i < 100; i++) {
                s.spawn(() -> {
                    try {
                        parking.incrementAndGet();
                        s.parkCurrent(null);
                    } finally {
                        unwound.incrementAndGet();
                    }
                });
            }
            awaitTrue(() -> parking.get() > 0, "a goroutine parked");
        } finally {
            SchedulerCore.install(prev);
        }
        List<Thread> stuck = s.terminate(10_000);
        assertTrue(stuck.isEmpty(), "carriers stopped: " + stuck);
        assertTrue(s.numGoroutines() == 0, "live goroutines after teardown: " + s.numGoroutines());
        assertTrue(s.carriersForTest().aliveCarriers() == 0 || allDead(s), "carrier threads ended");
        assertTrue(unwound.get() > 0, "admitted goroutines unwound through their finally blocks");
    }

    private static boolean allDead(SchedulerCore s) {
        return s.carriersForTest().stop().stream().noneMatch(Thread::isAlive);
    }
}
