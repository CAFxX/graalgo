package graalgo.p3link;

import graalgo.p3builtin.nodes.GoMaxProcsNode;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

/**
 * NEW-P1-1 (2026-10-15 review) regression tests: the P-id checkout pool
 * behind {@code sync.runtime_procPin}/{@code sync.runtime_procUnpin}.
 *
 * <p>gc pins the goroutine to its P and returns the P id, which is
 * exclusive per P by construction. The previous graalgo design handed out
 * per-thread dense ids reduced mod GOMAXPROCS; with more than GOMAXPROCS
 * threads in a run, congruent ids were concurrently pinned and shared one
 * {@code poolLocal} shard's {@code private} slot — a genuine Go data race
 * (two goroutines could observe the same {@code sync.Pool} object). The
 * fix checks ids out of a pool of exactly GOMAXPROCS: at most one thread
 * holds a given id at a time.
 *
 * <p>{@link #testConcurrentPinsHoldDistinctIds} is the stress test. Each
 * worker pins, asserts its id is in {@code [0, GOMAXPROCS)} and that no
 * other worker currently holds the same id, spins briefly to widen the
 * overlap window, then unpins. {@link #testPoolDrainsAfterStress} then
 * checks every id back out again, proving no id leaked (a leak would wedge
 * a later checkout, so the check is time-bounded and fails instead of
 * hanging).
 *
 * <p>Negative control: against the pre-fix classes (per-thread dense id
 * mod GOMAXPROCS), {@link #testConcurrentPinsHoldDistinctIds} fails —
 * with 4x GOMAXPROCS threads, each residue class has four threads holding
 * the same id, so two of them overlap in a pin section on the first
 * iterations and the duplicate-hold assertion fires. Post-fix the
 * checkout pool serializes the excess threads and the test passes.
 * {@link #testNestedPinReusesId} and
 * {@link #testUnbalancedUnpinIsNoop} pass both before and after: they pin
 * down the nesting contract shared by both designs.
 * {@link #testGomaxprocsShrinkGrow} covers the pool reconcile on
 * GOMAXPROCS changes: ids stay below the current GOMAXPROCS and a
 * grow-back never reissues an id that is still checked out.
 */
public final class ProcPinCheckoutTest {

    private static final long JOIN_TIMEOUT_MS = 120_000;
    private static final long DRAIN_TIMEOUT_MS = 30_000;
    /** Pin-section hold time per iteration: widens the overlap window. */
    private static final long HOLD_NANOS = 20_000;

    private static void spinNanos(long nanos) {
        long end = System.nanoTime() + nanos;
        while (System.nanoTime() < end) {
            // busy-wait: keep the pin section open so concurrent pins overlap
        }
    }

    private static void joinAll(Thread[] threads, String what) {
        for (Thread t : threads) {
            try {
                t.join(JOIN_TIMEOUT_MS);
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                throw new AssertionError(what + ": interrupted while joining", ie);
            }
            if (t.isAlive()) {
                throw new AssertionError(what + ": worker thread stuck (join timed out)");
            }
        }
    }

    public void testNestedPinReusesId() {
        SyncRuntime.clear();
        int outer = SyncRuntime.procPin();
        int nested = SyncRuntime.procPin();
        if (outer != nested) {
            throw new AssertionError(
                    "nested procPin must return the outermost id (gc semantics): outer="
                            + outer + " nested=" + nested);
        }
        SyncRuntime.procUnpin();
        SyncRuntime.procUnpin();
        // Balanced again after the full unpin: a fresh pin must succeed.
        int again = SyncRuntime.procPin();
        if (again < 0 || again >= GoMaxProcsNode.current()) {
            throw new AssertionError("procPin id out of range after nested unpin: " + again);
        }
        SyncRuntime.procUnpin();
    }

    public void testUnbalancedUnpinIsNoop() {
        SyncRuntime.clear();
        // No matching pin on this thread: must not throw and must not
        // corrupt the pool (a later balanced pin/unpin still works).
        SyncRuntime.procUnpin();
        int id = SyncRuntime.procPin();
        SyncRuntime.procUnpin();
        if (id < 0 || id >= GoMaxProcsNode.current()) {
            throw new AssertionError("procPin id out of range after unbalanced unpin: " + id);
        }
    }

    public void testIdsBoundedByGomaxprocs() {
        SyncRuntime.clear();
        int gmp = GoMaxProcsNode.current();
        int threads = Math.max(8, gmp * 4);
        AtomicReference<AssertionError> failure = new AtomicReference<>();
        Thread[] ts = new Thread[threads];
        for (int i = 0; i < threads; i++) {
            ts[i] = new Thread(() -> {
                try {
                    for (int k = 0; k < 50; k++) {
                        int id = SyncRuntime.procPin();
                        try {
                            if (id < 0 || id >= gmp) {
                                failure.compareAndSet(null, new AssertionError(
                                        "procPin id out of [0, GOMAXPROCS): " + id));
                                return;
                            }
                        } finally {
                            SyncRuntime.procUnpin();
                        }
                    }
                } catch (Throwable t) {
                    failure.compareAndSet(null, new AssertionError(t));
                }
            });
            ts[i].setDaemon(true);
            ts[i].start();
        }
        joinAll(ts, "testIdsBoundedByGomaxprocs");
        if (failure.get() != null) {
            throw failure.get();
        }
    }

    public void testConcurrentPinsHoldDistinctIds() throws Exception {
        SyncRuntime.clear();
        int gmp = GoMaxProcsNode.current();
        int threads = Math.max(8, gmp * 4);
        int iters = 100;
        ConcurrentHashMap<Integer, Boolean> held = new ConcurrentHashMap<>();
        AtomicReference<AssertionError> failure = new AtomicReference<>();
        CountDownLatch start = new CountDownLatch(1);
        Thread[] ts = new Thread[threads];
        for (int i = 0; i < threads; i++) {
            ts[i] = new Thread(() -> {
                try {
                    start.await();
                    for (int k = 0; k < iters && failure.get() == null; k++) {
                        int id = SyncRuntime.procPin();
                        try {
                            if (id < 0 || id >= gmp) {
                                failure.compareAndSet(null, new AssertionError(
                                        "procPin id out of [0, GOMAXPROCS): " + id));
                                return;
                            }
                            if (held.putIfAbsent(id, Boolean.TRUE) != null) {
                                // Two threads concurrently pinned with the
                                // same id: they would share one poolLocal
                                // shard's private slot. This is the race.
                                failure.compareAndSet(null, new AssertionError(
                                        "concurrent pin sections share id " + id));
                                return;
                            }
                            spinNanos(HOLD_NANOS);
                            held.remove(id);
                        } finally {
                            SyncRuntime.procUnpin();
                        }
                    }
                } catch (Throwable t) {
                    failure.compareAndSet(null, new AssertionError(t));
                }
            });
            ts[i].setDaemon(true);
            ts[i].start();
        }
        start.countDown();
        joinAll(ts, "testConcurrentPinsHoldDistinctIds");
        if (failure.get() != null) {
            throw failure.get();
        }
        assertPoolFullyDrained(gmp);
    }

    /**
     * After every worker has unpinned, all GOMAXPROCS ids must check out
     * again simultaneously: proves no id leaked (a leak would wedge the
     * checkout, so the wait is bounded and a leak fails the test instead
     * of hanging it).
     */
    private static void assertPoolFullyDrained(int gmp) throws Exception {
        CountDownLatch heldLatch = new CountDownLatch(gmp);
        CountDownLatch release = new CountDownLatch(1);
        int[] ids = new int[gmp];
        AtomicReference<Throwable> error = new AtomicReference<>();
        Thread[] ts = new Thread[gmp];
        for (int i = 0; i < gmp; i++) {
            final int k = i;
            ts[i] = new Thread(() -> {
                try {
                    ids[k] = SyncRuntime.procPin();
                    heldLatch.countDown();
                    if (!release.await(DRAIN_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
                        return;
                    }
                    SyncRuntime.procUnpin();
                } catch (Throwable t) {
                    error.compareAndSet(null, t);
                    heldLatch.countDown();
                }
            });
            ts[i].setDaemon(true);
            ts[i].start();
        }
        if (!heldLatch.await(DRAIN_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            release.countDown();
            joinAll(ts, "assertPoolFullyDrained");
            throw new AssertionError(
                    "pin pool did not drain: a checked-out id was never returned");
        }
        release.countDown();
        joinAll(ts, "assertPoolFullyDrained");
        if (error.get() != null) {
            throw new AssertionError(error.get());
        }
        Set<Integer> distinct = new HashSet<>();
        for (int id : ids) {
            if (id < 0 || id >= gmp) {
                throw new AssertionError("drained id out of range: " + id);
            }
            distinct.add(id);
        }
        if (distinct.size() != gmp) {
            throw new AssertionError(
                    "drained ids not distinct: " + distinct.size() + " of " + gmp);
        }
    }

    public void testPoolDrainsAfterStress() throws Exception {
        // Standalone drain check (also exercised at the end of the stress
        // test): ids are never lost across balanced pin/unpin cycles.
        SyncRuntime.clear();
        assertPoolFullyDrained(GoMaxProcsNode.current());
    }

    /**
     * GOMAXPROCS shrink/grow while an id is checked out: ids are only
     * ever issued below the current GOMAXPROCS, and a grow-back must not
     * reissue an id that is still checked out (the reconcile excludes
     * checked-out ids from the rebuilt free list).
     */
    public void testGomaxprocsShrinkGrow() throws Exception {
        SyncRuntime.clear();
        int saved = GoMaxProcsNode.current();
        try {
            setGomaxprocs(4);
            SyncRuntime.clear(); // reconcile to 4
            // Thread A holds an id across the shrink and the grow-back.
            CountDownLatch aPinned = new CountDownLatch(1);
            CountDownLatch aRelease = new CountDownLatch(1);
            AtomicReference<Integer> idA = new AtomicReference<>();
            AtomicReference<AssertionError> failure = new AtomicReference<>();
            Thread a = new Thread(() -> {
                int id = SyncRuntime.procPin();
                try {
                    idA.set(id);
                    aPinned.countDown();
                    if (!aRelease.await(DRAIN_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
                        failure.compareAndSet(null,
                                new AssertionError("holder A release timed out"));
                    }
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                    failure.compareAndSet(null, new AssertionError(ie));
                } finally {
                    SyncRuntime.procUnpin();
                }
            });
            a.setDaemon(true);
            a.start();
            if (!aPinned.await(DRAIN_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
                throw new AssertionError("holder A never pinned");
            }
            int heldA = idA.get();

            // Shrink to 2 while A holds its id: B must get an id in
            // [0, 2), distinct from A's even if A's id is now out of range.
            setGomaxprocs(2);
            int idB = SyncRuntime.procPin();
            try {
                if (idB < 0 || idB >= 2) {
                    throw new AssertionError("id after shrink out of [0,2): " + idB);
                }
                if (idB == heldA) {
                    throw new AssertionError("shrink reissued checked-out id " + heldA);
                }
            } finally {
                SyncRuntime.procUnpin();
            }

            // Grow back to 4 while A still holds its id: C must get an id
            // in [0, 4) that aliases neither A nor the drained B id.
            setGomaxprocs(4);
            int idC = SyncRuntime.procPin();
            try {
                if (idC < 0 || idC >= 4) {
                    throw new AssertionError("id after grow out of [0,4): " + idC);
                }
                if (idC == heldA) {
                    throw new AssertionError("grow reissued still-checked-out id " + heldA);
                }
            } finally {
                SyncRuntime.procUnpin();
            }

            aRelease.countDown();
            a.join(JOIN_TIMEOUT_MS);
            if (a.isAlive()) {
                throw new AssertionError("holder A stuck");
            }
            if (failure.get() != null) {
                throw failure.get();
            }
        } finally {
            setGomaxprocs(saved);
            SyncRuntime.clear();
        }
        assertPoolFullyDrained(GoMaxProcsNode.current());
    }

    private static void setGomaxprocs(int n) throws Exception {
        // GoMaxProcsNode exposes no setter (Go code changes the value
        // through the runtime.GOMAXPROCS wire node); the test reaches the
        // static cell directly to simulate a GOMAXPROCS change mid-run.
        java.lang.reflect.Field f = GoMaxProcsNode.class.getDeclaredField("CURRENT");
        f.setAccessible(true);
        ((AtomicInteger) f.get(null)).set(n);
    }
}
