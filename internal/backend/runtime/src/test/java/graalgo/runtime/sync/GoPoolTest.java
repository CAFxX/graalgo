package graalgo.runtime.sync;

import java.lang.reflect.Field;
import java.util.Collection;
import java.util.HashSet;
import java.util.Map;
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

    /**
     * Registry size, read reflectively so the test observes the
     * registry itself rather than a production test hook. Tolerates
     * the pre-fix Set shape and the fixed Map shape; either way the
     * size is the number of registered Locals.
     */
    private static int registrySize(GoPool pool) throws Exception {
        Field f = GoPool.class.getDeclaredField("allLocals");
        f.setAccessible(true);
        Object v = f.get(pool);
        if (v instanceof Map) {
            return ((Map<?, ?>) v).size();
        }
        if (v instanceof Collection) {
            return ((Collection<?>) v).size();
        }
        throw new AssertionError("allLocals is neither Map nor Collection: " + v.getClass());
    }

    /**
     * Dead-thread eviction (N-P1-7): 50 platform threads Put and die;
     * after the drain the registry holds only the live test thread,
     * and every item is still recovered — eviction drains dead Locals
     * into the bounded global queue instead of dropping their items.
     * Fails on the pre-fix registry (size stays 51: every dead thread
     * pinned forever) and on a drop-on-purge design (drain loses items).
     * Deterministic: eviction keys on thread termination (join), not on
     * GC, and the item count stays under the global cap, so no item can
     * legitimately vanish into the weakly-held victim.
     */
    public void testDeadThreadLocalsEvicted() throws Exception {
        GoPool pool = new GoPool();
        int threads = 50, perThread = 2, total = threads * perThread;
        Object[] items = new Object[total];
        for (int i = 0; i < total; i++) {
            items[i] = new Object();
        }
        for (int ti = 0; ti < threads; ti++) {
            final int base = ti * perThread;
            Thread t = new Thread(() -> {
                for (int i = 0; i < perThread; i++) {
                    pool.put(items[base + i]);
                }
            });
            t.start();
            t.join(10_000);
            assertTrue(!t.isAlive(), "worker terminated");
        }
        // Each worker's registration already evicted its dead
        // predecessors; the test thread's slow-path get evicts the last.
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < total; i++) {
            Object x = pool.get();
            assertTrue(x != null, "drain returned null at " + i);
            assertTrue(got.add(x), "duplicate item " + x);
        }
        assertEquals(total, got.size(), "drained multiset size");
        assertTrue(pool.get() == null, "pool drained");
        assertEquals(1, registrySize(pool), "registry holds only the live test thread after purge");
    }

    /**
     * Put-only bound (N-P1-7): 200 threads Put one item each and die
     * without any Get ever running, so only the registration purge can
     * bound the registry. Fails on the pre-fix registry (size 200
     * before the drain). The drain then recovers all 201 items,
     * proving eviction never drops a dead thread's items.
     */
    public void testPutOnlyRegistryBounded() throws Exception {
        GoPool pool = new GoPool();
        int threads = 200;
        Object[] items = new Object[threads + 1];
        for (int i = 0; i < items.length; i++) {
            items[i] = new Object();
        }
        for (int ti = 0; ti < threads; ti++) {
            final Object item = items[ti];
            Thread t = new Thread(() -> pool.put(item));
            t.start();
            t.join(10_000);
            assertTrue(!t.isAlive(), "worker terminated");
        }
        assertEquals(1, registrySize(pool), "put-only registry bounded before drain");
        Thread fresh = new Thread(() -> pool.put(items[threads]));
        fresh.start();
        fresh.join(10_000);
        assertTrue(!fresh.isAlive(), "fresh worker terminated");
        assertEquals(1, registrySize(pool), "put-only registry bounded after fresh registration");
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < items.length; i++) {
            Object x = pool.get();
            assertTrue(x != null, "drain returned null at " + i);
            assertTrue(got.add(x), "duplicate item " + x);
        }
        assertEquals(items.length, got.size(), "put-only drain size");
        assertTrue(pool.get() == null, "pool drained");
        assertEquals(1, registrySize(pool), "registry holds only the live test thread after drain");
    }

    /**
     * Virtual-thread variant of the eviction test (N-P1-7): goroutines
     * are virtual threads, unbounded unlike carrier threads, so this is
     * the reported shape — per-request virtual threads Put and die.
     * Same assertions: full drain plus a one-entry registry afterwards.
     */
    public void testVirtualThreadLocalsEvicted() throws Exception {
        GoPool pool = new GoPool();
        int threads = 50;
        Object[] items = new Object[threads];
        for (int i = 0; i < threads; i++) {
            items[i] = new Object();
        }
        for (int ti = 0; ti < threads; ti++) {
            final Object item = items[ti];
            Thread t = Thread.ofVirtual().start(() -> pool.put(item));
            t.join(10_000);
            assertTrue(!t.isAlive(), "virtual worker terminated");
        }
        Set<Object> got = new HashSet<>();
        for (int i = 0; i < threads; i++) {
            Object x = pool.get();
            assertTrue(x != null, "drain returned null at " + i);
            assertTrue(got.add(x), "duplicate item " + x);
        }
        assertEquals(threads, got.size(), "virtual drain size");
        assertTrue(pool.get() == null, "pool drained");
        assertEquals(1, registrySize(pool), "registry holds only the live test thread after virtual purge");
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
}
