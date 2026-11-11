package graalgo.runtime.sync;

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
     * Dead-thread locals are reclaimed (A-P1-1): N threads Put/Get through
     * one pool, die, and are dropped; after a forced GC their Locals must
     * be collectable and the pool's registered-Local count must return to
     * zero. Fails on the unpatched pool, where allLocals pins every Local
     * (plus up to 33 pooled objects each) for the pool's lifetime.
     * Virtual threads match production, where each goroutine runs on its
     * own vthread.
     */
    public void testDeadThreadLocalsReclaimed() throws Exception {
        GoPool pool = new GoPool();
        int n = 200;
        List<WeakReference<Object>> localRefs =
                Collections.synchronizedList(new ArrayList<>());
        List<Thread> threads = new ArrayList<>(n);
        for (int i = 0; i < n; i++) {
            threads.add(Thread.ofVirtual().start(() -> {
                pool.put(new Object());
                pool.get();
                localRefs.add(new WeakReference<>(pool.currentLocalForTest()));
            }));
        }
        for (Thread t : threads) {
            t.join(10_000);
            assertTrue(!t.isAlive(), "worker thread finished");
        }
        threads.clear();
        threads = null;
        assertEquals(n, localRefs.size(), "one Local handle per worker");
        // Poll for reclamation: a GC cycle must clear the weak refs and the
        // pool must deregister the Locals. Bounded wait; the Cleaner runs
        // asynchronously on its own thread.
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20);
        int cleared;
        do {
            System.gc();
            Thread.sleep(100);
            cleared = 0;
            for (WeakReference<Object> r : localRefs) {
                if (r.get() == null) {
                    cleared++;
                }
            }
        } while (cleared < n && System.nanoTime() < deadline);
        assertEquals(n, cleared,
                "dead-thread Locals collectable after GC (cleared " + cleared + " of " + n + ")");
        assertEquals(0, pool.registeredLocalCount(),
                "registered Local count after reclaim");
    }

    /**
     * Dead-thread items stay retrievable after reclaim (A-P1-1 semantics
     * pin): a thread Puts K items and dies with its Thread reference
     * dropped; once the pool has reclaimed its Local, the items must still
     * come back via the victim generation (gc's poolCleanup
     * primary-to-victim rotation). The items array keeps them strongly
     * reachable so the victim's weak refs stay valid. K spans the private
     * slot, the shared queue, and the global spillover, so the drain must
     * cover every retention site.
     */
    public void testDeadThreadItemsRetrievableAfterReclaim() throws Exception {
        GoPool pool = new GoPool();
        int k = 40;
        Object[] items = new Object[k];
        for (int i = 0; i < k; i++) {
            items[i] = new Object();
        }
        List<Thread> threads = new ArrayList<>(1);
        Thread t = Thread.ofVirtual().start(() -> {
            for (int i = 0; i < k; i++) {
                pool.put(items[i]);
            }
        });
        threads.add(t);
        t.join(10_000);
        assertTrue(!t.isAlive(), "producer thread finished");
        threads.clear();
        threads = null;
        t = null; // the method-local must not keep the Thread reachable:
        // a live reference delays the phantom (and hence the reclaim)
        // indefinitely, which is correct lifetime behavior, not a leak
        // Wait for the reclaim (Local deregistered) before draining, so
        // the items can only come back via the victim/global paths, not
        // via a direct steal from a still-registered Local.
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20);
        while (pool.registeredLocalCount() != 0 && System.nanoTime() < deadline) {
            System.gc();
            Thread.sleep(100);
        }
        assertEquals(0, pool.registeredLocalCount(),
                "producer Local reclaimed before drain");
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < k; i++) {
            Object x = pool.get();
            assertTrue(x != null, "retrieved item " + i + " after reclaim");
            got.add(x);
        }
        assertEquals(k, got.size(), "retrieved multiset size after reclaim");
    }
}
