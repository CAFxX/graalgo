package graalgo.runtime.sync;

import graalgo.runtime.GcInterop;

import java.lang.ref.WeakReference;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Unit tests for {@link GoPool} (P3-M11, the {@code sync.Pool} runtime).
 *
 * <p>Red-green discipline: each test fails against a broken pool (e.g. a
 * pool that drops items, a ThreadLocal-only pool with no cross-thread
 * stealing, a pool that throws on nil) and passes against the real one.
 * Go-level differential coverage lives in
 * {@code test/differential/corpus/46_sync_pool}.
 */
public final class GoPoolTest {

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

    /** Put/Get round-trip on one thread: the multiset is preserved. */
    public void testPutGetRoundTrip() {
        GoPool pool = new GoPool();
        pool.put("a");
        pool.put("b");
        pool.put("c");
        Set<Object> got = new HashSet<>();
        got.add(pool.get());
        got.add(pool.get());
        got.add(pool.get());
        assertEquals(3, got.size(), "round-trip multiset size");
        assertTrue(got.contains("a") && got.contains("b") && got.contains("c"),
                "round-trip multiset contents");
        assertTrue(pool.get() == null, "drained pool gets null");
    }

    /** Put(nil) is a no-op (gc parity); Get on empty is null. */
    public void testPutNilNoOp() {
        GoPool pool = new GoPool();
        pool.put(null);
        assertTrue(pool.get() == null, "put(nil) then get");
        assertTrue(pool.get() == null, "get on empty pool");
    }

    /** The New field slot round-trips (null default). */
    public void testNewFuncSlot() {
        GoPool pool = new GoPool();
        assertTrue(pool.getNewFunc() == null, "New defaults to null");
        Object f = new Object();
        pool.setNewFunc(f);
        assertTrue(pool.getNewFunc() == f, "New field round-trip");
        pool.setNewFunc(null);
        assertTrue(pool.getNewFunc() == null, "New field cleared");
    }

    /**
     * Cross-thread stealing: items Put by another (already-dead) thread
     * are visible to Get (gc's getSlow parity). Fails on a ThreadLocal-
     * only design with no allPools registry, and on a design that never
     * steals private slots. The items array keeps every item strongly
     * reachable (victim entries are weakly held by design).
     */
    public void testCrossThreadSteal() throws Exception {
        GoPool pool = new GoPool();
        int n = 16;
        Object[] items = new Object[n];
        for (int i = 0; i < n; i++) {
            items[i] = new Object();
        }
        Thread t = new Thread(() -> {
            for (int i = 0; i < n; i++) {
                pool.put(items[i]);
            }
        });
        t.start();
        t.join(10_000);
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < n; i++) {
            Object x = pool.get();
            assertTrue(x != null, "steal returned null at " + i);
            got.add(x);
        }
        assertEquals(n, got.size(), "stolen multiset size");
        assertTrue(pool.get() == null, "pool drained after steal");
    }

    /**
     * Concurrent hammer: 8 threads x 500 puts, then a single-threaded
     * drain recovers exactly every item — no loss, no duplication.
     * The items array keeps every item strongly reachable: victim
     * entries are weakly held by design (GC interaction), so without it
     * a GC mid-test could legitimately reclaim them.
     */
    public void testConcurrentNoLoss() throws Exception {
        GoPool pool = new GoPool();
        int threads = 8, perThread = 500, total = threads * perThread;
        Object[] items = new Object[total];
        for (int i = 0; i < total; i++) {
            items[i] = new Object();
        }
        CountDownLatch start = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(threads);
        for (int ti = 0; ti < threads; ti++) {
            final int base = ti * perThread;
            new Thread(() -> {
                try {
                    start.await(10, TimeUnit.SECONDS);
                } catch (InterruptedException e) {
                    throw new RuntimeException(e);
                }
                for (int i = 0; i < perThread; i++) {
                    pool.put(items[base + i]);
                }
                done.countDown();
            }).start();
        }
        start.countDown();
        assertTrue(done.await(60, TimeUnit.SECONDS), "putters finished");
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < total; i++) {
            Object x = pool.get();
            assertTrue(x != null, "concurrent drain returned null at " + i);
            assertTrue(got.add(x), "duplicate item " + x);
        }
        assertEquals(total, got.size(), "concurrent drain size");
        assertTrue(pool.get() == null, "pool drained");
    }

    /**
     * Spill path: far more items than the local/global caps still drain
     * completely (spillover to global, then the weakly-held victim, loses
     * nothing while the items are strongly reachable).
     */
    public void testSpillBounded() {
        GoPool pool = new GoPool();
        int n = 2000;
        Object[] items = new Object[n];
        for (int i = 0; i < n; i++) {
            items[i] = new Object();
            pool.put(items[i]);
        }
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < n; i++) {
            Object x = pool.get();
            assertTrue(x != null, "spill drain returned null at " + i);
            got.add(x);
        }
        assertEquals(n, got.size(), "spill drain size");
    }

    /**
     * RV-P1-24 primary probe (falsifiable): Locals of dead, unreachable
     * threads are reclaimed. N=2000 threads each register a Local, die,
     * and become unreachable; every Local must then be collected.
     * Negative control: run this test against the pre-fix GoPool (which
     * pins every Local in allLocals forever) and it fails — the weak
     * references never clear.
     */
    public void testDeadThreadLocalsReclaimed() throws Exception {
        GoPool pool = new GoPool();
        int n = 2000;
        List<WeakReference<Object>> localRefs =
                Collections.synchronizedList(new ArrayList<>(n));
        Thread[] threads = new Thread[n];
        for (int i = 0; i < n; i++) {
            threads[i] = new Thread(() -> {
                pool.put(new Object());
                pool.get();
                // Weak reference retains nothing: the Local is kept alive
                // only by the thread's ThreadLocalMap and by the pool's
                // registry — which the fix must release at unreachability.
                localRefs.add(new WeakReference<>(pool.debugLocal()));
            });
        }
        for (Thread t : threads) {
            t.start();
        }
        for (Thread t : threads) {
            t.join(30_000);
        }
        threads = null; // drop every Thread reference
        assertEquals(n, pool.registeredLocalCount(),
                "all Locals registered before GC");
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(60);
        while (System.nanoTime() < deadline) {
            boolean allCleared = true;
            for (WeakReference<Object> r : localRefs) {
                if (r.get() != null) {
                    allCleared = false;
                    break;
                }
            }
            if (allCleared && pool.registeredLocalCount() == 0) {
                return; // reclaimed: registry drained, Locals collected
            }
            GcInterop.gc();
        }
        throw new AssertionError(
                "dead-thread Locals not reclaimed within 60 s (leak)");
    }

    /**
     * RV-P1-24 semantics pin: the reaper observes thread *unreachability*,
     * not termination. A dead thread whose Thread object is still
     * referenced keeps its Local registered and all its items retrievable.
     * (testCrossThreadSteal covers the same shape; this pins the
     * no-reclamation-while-referenced half explicitly, including across
     * an explicit GC.)
     */
    public void testDeadThreadLocalKeptWhileThreadReferenced()
            throws Exception {
        GoPool pool = new GoPool();
        int n = 16;
        Object[] items = new Object[n];
        for (int i = 0; i < n; i++) {
            items[i] = new Object();
        }
        Thread t = new Thread(() -> {
            for (int i = 0; i < n; i++) {
                pool.put(items[i]);
            }
        });
        t.start();
        t.join(10_000);
        assertTrue(!t.isAlive(), "producer thread dead");
        GcInterop.gc();
        // Give any eager reaper a chance to fire; the Local must survive
        // because the Thread is still strongly referenced.
        Thread.sleep(2000);
        assertEquals(1, pool.registeredLocalCount(),
                "Local of referenced dead thread kept");
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < n; i++) {
            Object x = pool.get();
            assertTrue(x != null, "get returned null at " + i);
            got.add(x);
        }
        assertEquals(n, got.size(), "all dead-thread items retrievable");
        assertTrue(pool.get() == null, "pool drained");
    }

    /**
     * RV-P1-24 race torture: threads that put-then-die immediately (Thread
     * references are not retained, so the reaper drains their Locals to
     * the victim concurrently) while the main thread drains — no
     * exception, no lost or duplicated items. The items array keeps every
     * item strongly reachable so victim entries stay valid.
     */
    public void testDeathDuringStealTorture() throws Exception {
        GoPool pool = new GoPool();
        int threads = 32, perThread = 50, total = threads * perThread;
        Object[] items = new Object[total];
        for (int i = 0; i < total; i++) {
            items[i] = new Object();
        }
        CountDownLatch start = new CountDownLatch(1);
        for (int ti = 0; ti < threads; ti++) {
            final int base = ti * perThread;
            new Thread(() -> {
                try {
                    start.await(10, TimeUnit.SECONDS);
                } catch (InterruptedException e) {
                    throw new RuntimeException(e);
                }
                for (int i = 0; i < perThread; i++) {
                    pool.put(items[base + i]);
                }
                // die immediately; no Thread reference is retained
            }).start();
        }
        start.countDown();
        Set<Object> got = new HashSet<>();
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(120);
        while (got.size() < total) {
            Object x = pool.get();
            if (x != null) {
                assertTrue(got.add(x), "duplicate item");
            } else if (System.nanoTime() >= deadline) {
                throw new AssertionError(
                        "lost items: got " + got.size() + " of " + total);
            } else {
                Thread.sleep(5);
            }
        }
        assertTrue(pool.get() == null, "pool drained");
    }
}
