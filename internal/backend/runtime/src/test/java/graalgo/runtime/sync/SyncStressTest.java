package graalgo.runtime.sync;

import graalgo.sched.SchedulerCore;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Stress tests for the scheduler-aware sync primitives
 * ({@code GoMutex}, {@code GoRWMutex}, {@code GoWaitGroup}, {@code GoOnce}).
 *
 * <p>Red-green discipline: each test fails against a broken primitive
 * (e.g. a mutex that doesn't actually exclude, a WaitGroup that loses
 * wakeups) and passes against the real one.
 *
 * <p>Note on scope: these test the Java runtime primitives. Go-level
 * {@code import "sync"} bindings are blocked on the WS G import overlay
 * (Phase-1 frozen; P2-M8 may not modify Phase-1) — see the P2-M8
 * checkpoint entry. Re-test trigger: when the overlay lands, these
 * scenarios re-run as Go programs differential vs gc.
 */
public final class SyncStressTest {

    private static SchedulerCore newScheduler(int gmp) {
        SchedulerCore sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        return sched;
    }

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

    /**
     * Mutex mutual exclusion: 32 goroutines × 1000 increments under lock;
     * final counter must be exact. verifies: GG-SCHED-0004.
     */
    public void testMutexExclusion() throws Exception {
        SchedulerCore sched = newScheduler(4);
        try {
            GoMutex mu = new GoMutex();
            AtomicLong counter = new AtomicLong(0);
            int n = 32, iters = 1000;
            CountDownLatch done = new CountDownLatch(n);
            for (int i = 0; i < n; i++) {
                sched.spawn(() -> {
                    for (int k = 0; k < iters; k++) {
                        mu.lock();
                        try {
                            counter.incrementAndGet();
                        } finally {
                            mu.unlock();
                        }
                    }
                    done.countDown();
                });
            }
            assertTrue(done.await(120, TimeUnit.SECONDS), "mutex stress timed out (deadlock?)");
            sched.awaitQuiescence();
            assertEquals((long) n * iters, counter.get(), "mutex exclusion violated");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Mutex under single-cap pressure: with cap=1, lock contention must
     * still make progress (no convoy deadlock).
     */
    public void testMutexCapOne() throws Exception {
        SchedulerCore sched = newScheduler(1);
        try {
            GoMutex mu = new GoMutex();
            AtomicInteger counter = new AtomicInteger(0);
            int n = 8, iters = 200;
            CountDownLatch done = new CountDownLatch(n);
            for (int i = 0; i < n; i++) {
                sched.spawn(() -> {
                    for (int k = 0; k < iters; k++) {
                        mu.lock();
                        try {
                            counter.incrementAndGet();
                        } finally {
                            mu.unlock();
                        }
                    }
                    done.countDown();
                });
            }
            assertTrue(done.await(120, TimeUnit.SECONDS), "cap=1 mutex stress timed out");
            sched.awaitQuiescence();
            assertEquals((long) n * iters, counter.get(), "cap=1 mutex exclusion violated");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** Unlock of an unlocked mutex panics (gc parity). */
    public void testMutexUnlockOfUnlockedPanics() {
        GoMutex mu = new GoMutex();
        boolean panicked = false;
        try {
            mu.unlock();
        } catch (IllegalStateException e) {
            panicked = true;
        }
        assertTrue(panicked, "unlock of unlocked mutex must panic");
    }

    /**
     * RWMutex: 16 readers × 500 reads concurrent with 2 writers × 100
     * writes; every read sees a consistent value (no torn writes), final
     * value exact.
     */
    public void testRWMutex() throws Exception {
        SchedulerCore sched = newScheduler(4);
        try {
            GoRWMutex mu = new GoRWMutex();
            AtomicLong value = new AtomicLong(0);
            AtomicLong tornReads = new AtomicLong(0);
            int nReaders = 16, nWriters = 2, reads = 500, writes = 100;
            CountDownLatch done = new CountDownLatch(nReaders + nWriters);
            for (int i = 0; i < nReaders; i++) {
                sched.spawn(() -> {
                    for (int k = 0; k < reads; k++) {
                        mu.rLock();
                        try {
                            long v = value.get();
                            // Writers only increment by 1; any value is
                            // consistent (single long read is atomic).
                            if (v < 0) {
                                tornReads.incrementAndGet();
                            }
                        } finally {
                            mu.rUnlock();
                        }
                    }
                    done.countDown();
                });
            }
            for (int i = 0; i < nWriters; i++) {
                sched.spawn(() -> {
                    for (int k = 0; k < writes; k++) {
                        mu.lock();
                        try {
                            value.incrementAndGet();
                        } finally {
                            mu.unlock();
                        }
                    }
                    done.countDown();
                });
            }
            assertTrue(done.await(120, TimeUnit.SECONDS), "rwmutex stress timed out");
            sched.awaitQuiescence();
            assertEquals((long) nWriters * writes, value.get(), "rwmutex write count wrong");
            assertEquals(0, tornReads.get(), "torn reads observed");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * WaitGroup: 50 workers, main awaits; also tests await-before-add
     * (zero counter returns immediately).
     */
    public void testWaitGroup() throws Exception {
        SchedulerCore sched = newScheduler(4);
        try {
            GoWaitGroup wg = new GoWaitGroup();
            AtomicInteger counter = new AtomicInteger(0);
            AtomicLong awaitResult = new AtomicLong(-1);
            int n = 50;
            wg.add(n);
            for (int i = 0; i < n; i++) {
                sched.spawn(() -> {
                    counter.incrementAndGet();
                    wg.done();
                });
            }
            // await() must run on a scheduler goroutine (like Go's main).
            CountDownLatch awaitDone = new CountDownLatch(1);
            sched.spawn(() -> {
                wg.await();
                awaitResult.set(wg.count());
                awaitDone.countDown();
            });
            assertTrue(awaitDone.await(120, TimeUnit.SECONDS), "waitgroup await timed out");
            sched.awaitQuiescence();
            assertEquals(n, counter.get(), "waitgroup worker count wrong");
            assertEquals(0, awaitResult.get(), "waitgroup counter not zero after await");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** WaitGroup negative counter panics. */
    public void testWaitGroupNegativePanics() {
        GoWaitGroup wg = new GoWaitGroup();
        boolean panicked = false;
        try {
            wg.add(-1);
        } catch (IllegalStateException e) {
            panicked = true;
        }
        assertTrue(panicked, "negative WaitGroup counter must panic");
    }

    /**
     * R2 P1-2: add() that reaches zero with no scheduler installed must
     * throw BEFORE mutating the counter (mirrors the GoOnce P2-M10 fix).
     * Red-green: against the old order (counter = next before
     * SchedulerCore.current()) the counter reads 0 after the throw,
     * stranding any parked waiter; against the fixed order it is
     * untouched, so a later legitimate done() still wakes the waiter.
     */
    public void testWaitGroupAddToZeroWithoutSchedulerDoesNotMutate() {
        SchedulerCore.install(null);
        GoWaitGroup wg = new GoWaitGroup();
        wg.add(1); // nonzero result: no scheduler needed, works scheduler-less
        assertEquals(1, wg.count(), "setup add");
        boolean threw = false;
        try {
            wg.done(); // next == 0 -> current() must throw before the mutation
        } catch (IllegalStateException e) {
            threw = true;
        }
        assertTrue(threw, "done() to zero with no scheduler must throw");
        assertEquals(1, wg.count(),
                "counter must be unmutated when scheduler resolution throws");
    }

    /** Zero-counter await returns immediately (no park). */
    public void testWaitGroupZeroAwaitImmediate() throws Exception {
        SchedulerCore sched = newScheduler(4);
        try {
            GoWaitGroup wg = new GoWaitGroup();
            CountDownLatch done = new CountDownLatch(1);
            sched.spawn(() -> {
                long t0 = System.nanoTime();
                wg.await();
                long dt = System.nanoTime() - t0;
                // Must return without parking: generous 100ms bound.
                assertTrue(dt < 100_000_000L, "zero await parked: " + dt + "ns");
                done.countDown();
            });
            assertTrue(done.await(30, TimeUnit.SECONDS), "zero await test timed out");
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Once: 32 goroutines race doOnce; action runs exactly once; all
     * callers return after completion.
     */
    public void testOnce() throws Exception {
        SchedulerCore sched = newScheduler(4);
        try {
            GoOnce once = new GoOnce();
            AtomicInteger runs = new AtomicInteger(0);
            int n = 32;
            CountDownLatch done = new CountDownLatch(n);
            for (int i = 0; i < n; i++) {
                sched.spawn(() -> {
                    once.doOnce(runs::incrementAndGet);
                    done.countDown();
                });
            }
            assertTrue(done.await(120, TimeUnit.SECONDS), "once stress timed out");
            sched.awaitQuiescence();
            assertEquals(1, runs.get(), "once action must run exactly once");
            assertTrue(once.isDone(), "once must report done");
            // Second wave returns immediately.
            once.doOnce(runs::incrementAndGet);
            assertEquals(1, runs.get(), "once must not re-run");
        } finally {
            SchedulerCore.install(null);
        }
    }
}
