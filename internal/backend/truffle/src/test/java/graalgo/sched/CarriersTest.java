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
 * test that fails when the mechanism is removed. Carrier mode is the default (GG-0074), so the
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

    /**
     * A carrier started but not yet running its loop when the pool stops is still stopped and
     * joined: {@code stop()} returns it and it ends at once (CI: "the language did not complete
     * all polyglot threads" after a program exit with a goroutine left blocked, the carrier
     * started by the teardown's unpark raced the stop). The gate holds every new carrier thread
     * before its loop, so the race is forced, not sampled. Mutation: a carrier that registers
     * itself from its loop — stop() misses it and it idles on, red.
     */
    public void testStopIncludesCarriersNotYetRunning() throws Exception {
        CountDownLatch gate = new CountDownLatch(1);
        List<Thread> created = java.util.Collections.synchronizedList(new ArrayList<>());
        SchedulerCore s = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS, r -> {
            Thread t = Thread.ofPlatform().unstarted(() -> {
                try {
                    gate.await();
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
                r.run();
            });
            created.add(t);
            return t;
        }, null, true);
        s.start();
        SchedulerCore prev = SchedulerCore.install(s);
        try {
            s.spawn(() -> { });
            awaitTrue(() -> !created.isEmpty(), "a carrier started");
        } finally {
            SchedulerCore.install(prev);
        }
        List<Thread> stopped = s.carriersForTest().stop();
        gate.countDown();
        List<Thread> snapshot;
        synchronized (created) {
            snapshot = new ArrayList<>(created);
        }
        assertTrue(stopped.containsAll(snapshot), "stop() returned every started carrier: " + stopped
                + " of " + snapshot);
        for (Thread t : snapshot) {
            assertTrue(t.join(java.time.Duration.ofSeconds(5)), "carrier ended after stop: " + t);
        }
        s.terminate(1_000);
    }

    /**
     * BUG-0055: a waker that read PINNED, then lost the CPU while the pinned goroutine took its
     * permit without its wake (a spurious return, or a sampler's thread-local action interrupting
     * the Truffle-blocked park), ran to completion and had {@code run()} clear {@code carrier},
     * dereferenced the null carrier — an NPE on the drainer, whose fail-loud handler halts the
     * JVM. The waker's stale view is forced, not sampled: the handle is put in the state the
     * waker sees after its state read (PINNED, no permit, carrier already cleared). Mutation:
     * {@code h.carrier.thread} without the null check — red (NPE).
     */
    public void testUnparkOfPinnedGoroutineThatEndedMeanwhile() throws Exception {
        SchedulerCore s = carrierScheduler(1);
        try {
            GoroutineHandle h = new GoroutineHandle(() -> { });
            h.runState = Carriers.PINNED;
            h.permit = false;
            h.carrier = null;
            s.carriersForTest().unpark(h, false);
            assertTrue(h.permit, "the permit is granted (the goroutine, if it parks again, sees it)");
        } finally {
            assertTrue(s.terminate(10_000).isEmpty(), "carriers stopped");
        }
    }

    /**
     * BUG-0056: a goroutine spawned after {@code terminateCarriers} snapshotted the live list was
     * never unparked (the drainer is stopped), so the teardown waited its whole deadline (10 s
     * for a closing context) for a goroutine that could never run. Forced interleaving: the
     * spawner is running (blocked on a latch) when terminate starts; once terminate's unpark of
     * the snapshot reached it (its permit is set, so the snapshot is taken) it spawns. Fixed,
     * that spawn is refused with the teardown unwind and terminate returns at once. Mutation:
     * no terminated check in spawn — red (terminate takes the full 10 s, a goroutine stays live).
     */
    public void testSpawnRacingTerminateDoesNotStallTeardown() throws Exception {
        SchedulerCore s = carrierScheduler(1);
        SchedulerCore prev = SchedulerCore.install(s);
        CountDownLatch inBody = new CountDownLatch(1);
        CountDownLatch spawnNow = new CountDownLatch(1);
        AtomicInteger refused = new AtomicInteger();
        GoroutineHandle spawner;
        try {
            spawner = s.spawn(() -> {
                inBody.countDown();
                try {
                    spawnNow.await();
                } catch (InterruptedException e) {
                    throw new IllegalStateException(e);
                }
                try {
                    s.spawn(() -> { });
                } catch (SchedulerCore.GoroutineTerminated t) {
                    refused.incrementAndGet();
                    throw t;
                }
            });
            assertTrue(inBody.await(20, TimeUnit.SECONDS), "spawner running");
            // The admission that started the spawner left its permit set (a running goroutine
            // consumes a permit only when it parks): cleared, so only terminate's unpark of its
            // live-list snapshot sets it again. Nothing else wakes a goroutine running its body.
            spawner.permit = false;
        } finally {
            SchedulerCore.install(prev);
        }
        List<List<Thread>> stuck = java.util.Collections.synchronizedList(new ArrayList<>());
        Thread term = Thread.ofPlatform().start(() -> {
            try {
                stuck.add(s.terminate(10_000));
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        });
        long t0 = System.nanoTime();
        awaitTrue(() -> s.terminatedForTest() && spawner.permit, "terminate's live-list snapshot reached the spawner");
        spawnNow.countDown();
        assertTrue(term.join(java.time.Duration.ofSeconds(20)), "terminate returned");
        long ms = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - t0);
        assertTrue(ms < 1_000, "terminate took " + ms + " ms (waited for a goroutine that cannot run)");
        assertTrue(stuck.size() == 1 && stuck.get(0).isEmpty(), "carriers stopped: " + stuck);
        assertTrue(s.numGoroutines() == 0, "live goroutines after teardown: " + s.numGoroutines());
        assertTrue(refused.get() == 1, "the late spawn was refused");
    }

    /**
     * BUG-0050: a Truffle-blocked goroutine park ({@link Carriers#PARK_INTERRUPTIBLY}: thread
     * mode, and a pinned park) whose wake and a thread-local action's interrupt (CPU sampler,
     * debugger) arrived together threw for the interrupt and lost the wake: Truffle runs the
     * action and calls the interruptible again, which parked with no permit left — forever,
     * although the waker had published the goroutine's admission (a sampled thread-mode program
     * never finished). Truffle's re-invocation contract is driven directly, so the interleaving
     * is forced: permit and interrupt pending, first call, then the second call. Mutation: the
     * second call parks again — red (it never returns).
     */
    public void testInterruptedParkDoesNotLoseTheWake() throws Exception {
        GoroutineHandle h = new GoroutineHandle(() -> { });
        java.util.concurrent.atomic.AtomicReference<String> problem = new java.util.concurrent.atomic.AtomicReference<>();
        Thread t = Thread.ofPlatform().start(() -> {
            java.util.concurrent.locks.LockSupport.unpark(Thread.currentThread()); // the waker
            Thread.currentThread().interrupt(); // a thread-local action's delivery
            try {
                Carriers.PARK_INTERRUPTIBLY.apply(h);
                problem.set("the interrupt was not reported to Truffle");
            } catch (InterruptedException expected) {
                // Truffle runs the action, then calls the interruptible again.
            }
            try {
                Carriers.PARK_INTERRUPTIBLY.apply(h);
            } catch (InterruptedException e) {
                problem.set("no interrupt is pending: " + e);
            }
        });
        boolean returned = t.join(java.time.Duration.ofSeconds(5));
        if (!returned) {
            java.util.concurrent.locks.LockSupport.unpark(t);
            t.join();
        }
        assertTrue(returned, "the re-invoked park returned (the wake was not lost)");
        assertTrue(problem.get() == null, String.valueOf(problem.get()));
    }

    /**
     * A goroutine never parks holding the pin lock (review round 4, finding 5): the lock is owned
     * by the carrier thread, so a yield inside a pin section would let the next goroutine on that
     * carrier re-enter the section (the lock is reentrant) and block every other carrier's. The
     * park fails loud instead. Mutation: no check in {@code Carriers.park} — red (the park goes
     * ahead with the lock held).
     */
    public void testParkInsidePinSectionFailsLoud() throws Exception {
        SchedulerCore s = carrierScheduler(1);
        SchedulerCore prev = SchedulerCore.install(s);
        java.util.concurrent.atomic.AtomicReference<Throwable> got = new java.util.concurrent.atomic.AtomicReference<>();
        CountDownLatch done = new CountDownLatch(1);
        try {
            s.spawn(() -> {
                graalgo.runtime.sync.PinLock.LOCK.lock();
                try {
                    s.carriersForTest().park(s.currentGoroutine());
                } catch (IllegalStateException e) {
                    got.set(e);
                } finally {
                    graalgo.runtime.sync.PinLock.LOCK.unlock();
                    done.countDown();
                }
            });
            assertTrue(done.await(5, TimeUnit.SECONDS), "the park inside the pin section returned");
            assertTrue(got.get() != null, "the park failed loud");
            assertTrue(!graalgo.runtime.sync.PinLock.LOCK.isLocked(), "pin lock released");
        } finally {
            SchedulerCore.install(prev);
            assertTrue(s.terminate(10_000).isEmpty(), "carriers stopped");
        }
    }

    private static boolean allDead(SchedulerCore s) {
        return s.carriersForTest().stop().stream().noneMatch(Thread::isAlive);
    }
}
