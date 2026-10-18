package graalgo.p3link;

import graalgo.frontend.FrontendException;
import graalgo.p3builtin.nodes.GoMaxProcsNode;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

/**
 * R-4 regression tests: the {@code procPin} id-checkout pool.
 *
 * <p>The pre-fix implementation handed out per-thread dense ids reduced
 * mod GOMAXPROCS, so once more than GOMAXPROCS threads existed, two
 * threads could hold congruent ids in pin sections <em>concurrently</em>
 * and share one {@code poolLocal} shard's {@code private} slot — a
 * genuine Go data race on the real {@code sync.Pool} path. The checkout
 * pool hands each concurrent pin section a unique id in
 * {@code [0, GOMAXPROCS)}.
 *
 * <p>Negative control: {@link #testPinIdsUniqueUnderHammer} fails against
 * the pre-fix classes (congruent ids are observed held concurrently);
 * all tests pass against the checkout pool.
 *
 * <p>Each test saves and restores the process-observable GOMAXPROCS and
 * calls {@link SyncRuntime#clear()} afterwards, so the shared-JVM test
 * runner sees no leakage.
 */
public final class SyncRuntimePinTest {

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

    /** Runs {@code body} with GOMAXPROCS forced to {@code gmp}, restoring afterwards. */
    private static void withGmp(int gmp, Runnable body) {
        int prev = GoMaxProcsNode.current();
        GoMaxProcsNode.setInitial(gmp);
        try {
            body.run();
        } finally {
            GoMaxProcsNode.setInitial(prev);
            SyncRuntime.clear();
        }
    }

    /**
     * Hammer: 32 threads (8x GOMAXPROCS) pin/unpin in a tight loop. Every
     * observed id must be in {@code [0, GOMAXPROCS)}, and no id may ever
     * be held by two threads at once (the pre-fix race).
     */
    public void testPinIdsUniqueUnderHammer() throws Exception {
        withGmp(4, () -> {
            int threads = 32;
            int iters = 300;
            ConcurrentHashMap<Integer, Boolean> held = new ConcurrentHashMap<>();
            CountDownLatch start = new CountDownLatch(1);
            CountDownLatch done = new CountDownLatch(threads);
            AtomicReference<Throwable> failure = new AtomicReference<>();
            for (int t = 0; t < threads; t++) {
                Thread th = new Thread(() -> {
                    try {
                        start.await();
                        for (int i = 0; i < iters; i++) {
                            int id = SyncRuntime.procPin();
                            try {
                                if (id < 0 || id >= 4) {
                                    throw new AssertionError(
                                            "pin id out of range: " + id);
                                }
                                if (held.putIfAbsent(id, Boolean.TRUE) != null) {
                                    throw new AssertionError(
                                            "pin id " + id + " held concurrently by two threads");
                                }
                                // Widen the race window: spin while holding.
                                for (int s = 0; s < 50; s++) {
                                    Thread.onSpinWait();
                                }
                                held.remove(id);
                            } finally {
                                SyncRuntime.procUnpin();
                            }
                        }
                    } catch (Throwable e) {
                        failure.compareAndSet(null, e);
                    } finally {
                        done.countDown();
                    }
                });
                th.setDaemon(true);
                th.start();
            }
            start.countDown();
            try {
                done.await();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new AssertionError("interrupted", e);
            }
            if (failure.get() != null) {
                throw new AssertionError("hammer failed", failure.get());
            }
        });
    }

    /**
     * Nested pin sections on one thread reuse the outer id (gc pins to
     * the same P); only the outermost unpin returns it to the pool.
     */
    public void testNestedPinReusesId() {
        withGmp(2, () -> {
            int outer = SyncRuntime.procPin();
            int inner = SyncRuntime.procPin();
            assertEquals(outer, inner, "nested pin must reuse the outer id");
            SyncRuntime.procUnpin();
            SyncRuntime.procUnpin(); // outermost: id returns to the pool
            int again = SyncRuntime.procPin();
            assertTrue(again >= 0 && again < 2, "id in range after full unpin");
            SyncRuntime.procUnpin();
        });
    }

    /**
     * Unbalanced unpin fails fast with a named cause — never silently
     * corrupts the pool.
     */
    public void testUnbalancedUnpinFailsFast() {
        withGmp(2, () -> {
            boolean failed = false;
            try {
                SyncRuntime.procUnpin();
            } catch (FrontendException e) {
                failed = e.getMessage().contains("procUnpin");
            }
            assertTrue(failed, "unpin with no pin must fail fast");
        });
    }

    /**
     * The pool tracks GOMAXPROCS: after a change, new checkouts (on
     * threads with no pin held) come from a generation sized by the new
     * value, and ids stay in range. The old generation's outstanding
     * checkout is unaffected.
     */
    public void testPoolTracksGomaxprocsChange() {
        withGmp(2, () -> {
            int id2 = SyncRuntime.procPin();
            assertTrue(id2 >= 0 && id2 < 2, "id in range for gmp=2");
            GoMaxProcsNode.setInitial(6);
            AtomicInteger id6 = new AtomicInteger(-1);
            AtomicReference<Throwable> failure = new AtomicReference<>();
            Thread th = new Thread(() -> {
                try {
                    int id = SyncRuntime.procPin();
                    id6.set(id);
                    SyncRuntime.procUnpin();
                } catch (Throwable e) {
                    failure.compareAndSet(null, e);
                }
            });
            th.setDaemon(true);
            th.start();
            try {
                th.join(5000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new AssertionError("interrupted", e);
            }
            if (failure.get() != null) {
                throw new AssertionError("pin on new generation failed", failure.get());
            }
            assertTrue(id6.get() >= 0 && id6.get() < 6,
                    "new-generation id in range for gmp=6, got " + id6.get());
            // The old checkout still validates against its own generation.
            SyncRuntime.procUnpin();
            int fresh = SyncRuntime.procPin();
            assertTrue(fresh >= 0 && fresh < 6, "id in range after drain");
            SyncRuntime.procUnpin();
        });
    }

    /**
     * Pool exhaustion (all ids checked out, e.g. leaked by unbalanced
     * pins) fails fast with a named cause instead of aliasing ids.
     */
    public void testExhaustionFailsFast() {
        withGmp(1, () -> {
            int id = SyncRuntime.procPin();
            assertEquals(0, id, "single id pool hands out 0");
            // Nested pin reuses the id (no exhaustion); leak it by pinning
            // on a second thread instead.
            AtomicInteger observed = new AtomicInteger(-1);
            AtomicReference<Throwable> failure = new AtomicReference<>();
            Thread th = new Thread(() -> {
                try {
                    observed.set(SyncRuntime.procPin());
                } catch (Throwable e) {
                    failure.set(e);
                }
            });
            th.setDaemon(true);
            th.start();
            try {
                th.join(5000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            assertTrue(failure.get() instanceof FrontendException,
                    "exhausted pool must fail fast, got: " + failure.get());
            assertTrue(failure.get().getMessage().contains("exhausted"),
                    "failure must name exhaustion: " + failure.get().getMessage());
            SyncRuntime.procUnpin();
        });
    }
}
