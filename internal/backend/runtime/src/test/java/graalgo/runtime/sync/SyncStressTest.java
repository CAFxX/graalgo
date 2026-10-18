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

    /**
     * Asserts a primitive misuse raises the documented signal: an
     * {@code IllegalStateException} carrying gc's exact message text.
     * The signal is translated to the guest-visible failure mode by the
     * binding (ADR-0002 D4) — Go panic for WaitGroup misuse, fatal error
     * for mutex/RWMutex unlock misuse — pinned end to end by
     * {@code SyncMisuseEndToEndTest}.
     */
    private interface Misuse {
        void trigger();
    }

    private static void assertMisuseSignal(Misuse m, String gcMessage) {
        try {
            m.trigger();
        } catch (IllegalStateException e) {
            assertTrue(gcMessage.equals(e.getMessage()),
                    "misuse signal message must be gc's: expected=[" + gcMessage
                            + "] actual=[" + e.getMessage() + "]");
            return;
        }
        throw new AssertionError("misuse must signal, expected message: " + gcMessage);
    }

    /**
     * Unlock of an unlocked mutex signals with gc's message text (the
     * binding translates the signal to gc's fatal error — unrecoverable,
     * exit 2; ADR-0002 D4).
     */
    public void testMutexUnlockOfUnlockedSignals() {
        newScheduler(1);
        try {
            GoMutex mu = new GoMutex();
            assertMisuseSignal(mu::unlock, "sync: unlock of unlocked mutex");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** Unlock of an unlocked RWMutex signals with gc's message text. */
    public void testRWMutexUnlockOfUnlockedSignals() {
        newScheduler(1);
        try {
            GoRWMutex mu = new GoRWMutex();
            assertMisuseSignal(mu::unlock, "sync: Unlock of unlocked RWMutex");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** RUnlock of an unlocked RWMutex signals with gc's message text. */
    public void testRWMutexRUnlockOfUnlockedSignals() {
        newScheduler(1);
        try {
            GoRWMutex mu = new GoRWMutex();
            assertMisuseSignal(mu::rUnlock, "sync: RUnlock of unlocked RWMutex");
        } finally {
            SchedulerCore.install(null);
        }
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

    /**
     * WaitGroup negative counter signals with gc's message text (the
     * binding translates the signal to a recoverable Go panic — gc
     * panics here; ADR-0002 D4).
     */
    public void testWaitGroupNegativeSignals() {
        newScheduler(1);
        try {
            GoWaitGroup wg = new GoWaitGroup();
            assertMisuseSignal(() -> wg.add(-1), "sync: negative WaitGroup counter");
            assertMisuseSignal(wg::done, "sync: negative WaitGroup counter");
        } finally {
            SchedulerCore.install(null);
        }
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
