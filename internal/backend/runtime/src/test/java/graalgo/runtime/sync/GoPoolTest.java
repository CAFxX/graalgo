package graalgo.runtime.sync;

import java.lang.reflect.Field;
import java.util.Deque;
import java.util.HashSet;
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
     * Concurrent hammer: 8 threads x 150 puts, then a single-threaded
     * drain recovers exactly every item — no loss, no duplication.
     * The per-thread count keeps the total (1200) within the pool's total
     * capacity (8 x (1 private + 32 shared) + 256 global + 1024 victim =
     * 1544), so the no-loss assertion stays a valid race probe: drops can
     * only happen past capacity (sync.Pool's contract permits dropping
     * any item at any time), and 1200 items can never fill global+victim
     * past the 1280 drop threshold. The items array keeps every item
     * strongly reachable: victim entries are weakly held by design (GC
     * interaction), so without it a GC mid-test could legitimately
     * reclaim them.
     */
    public void testConcurrentNoLoss() throws Exception {
        GoPool pool = new GoPool();
        int threads = 8, perThread = 150, total = threads * perThread;
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
     * Spill path: more items than the local/global caps still drain
     * completely when within the pool's total capacity (spillover to
     * global, then the weakly-held victim, loses nothing while the items
     * are strongly reachable and the victim cap is not reached: 1000 <
     * 1 private + 32 shared + 256 global + 1024 victim = 1313).
     */
    public void testSpillBounded() {
        GoPool pool = new GoPool();
        int n = 1000;
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

    /** Reads the private victim deque size via reflection: a red-green
     * probe for the victim bound that adds no production API surface
     * (reflection precedent: {@code JfrEventsTest}).
     * Single-threaded use only — no pool access races the read. */
    private static int victimSize(GoPool pool) throws Exception {
        Field f = GoPool.class.getDeclaredField("victim");
        f.setAccessible(true);
        @SuppressWarnings("unchecked")
        Deque<Object> victim = (Deque<Object>) f.get(pool);
        return victim.size();
    }

    /**
     * Must match {@code GoPool.VICTIM_CAP} (and the 1/32/256 below must
     * match its private-slot / local-shared / global caps): literals, not
     * reflection, so the pre-fix red is the size assertion itself, and
     * any drift of the production constants fails loudly here instead of
     * silently redefining the test.
     */
    private static final int EXPECTED_VICTIM_CAP = 1024;

    /**
     * Victim boundedness (R-1): sustained Puts with no Gets keep the
     * victim deque bounded. The victim holds only WeakReference wrappers,
     * so the wrapper count is the bounded quantity. Items are retained in
     * the array so no referent can be reclaimed mid-test — the measurement
     * is deterministic and GC-independent (no GC timing assumed). Fails on
     * the pre-fix design, whose victim grew without bound under this exact
     * workload.
     */
    public void testVictimBounded() throws Exception {
        GoPool pool = new GoPool();
        int n = 50_000;
        Object[] items = new Object[n];
        for (int i = 0; i < n; i++) {
            items[i] = new Object();
            pool.put(items[i]);
        }
        int size = victimSize(pool);
        assertTrue(size <= EXPECTED_VICTIM_CAP,
                "victim bounded after " + n + " puts: size=" + size
                        + " cap=" + EXPECTED_VICTIM_CAP);
        // Guard against a vacuous pass: n dwarfs every cap, so the spill
        // path must actually have run and the deque must sit AT the cap.
        assertTrue(size == EXPECTED_VICTIM_CAP,
                "victim filled to the cap (spill path exercised): size=" + size
                        + " cap=" + EXPECTED_VICTIM_CAP);
    }

    /**
     * Eviction integrity: puts far beyond total capacity (3000 > 1 + 32 +
     * 256 + 1024 = 1313) force victim eviction. sync.Pool's contract
     * permits dropping any item at any time, so the pool need not retain
     * everything — but it must never duplicate, never return a foreign
     * item, must drain cleanly to null, and must retain a bounded,
     * substantial subset (at least the full victim cap: eviction drops
     * oldest-first, it never discards wholesale). Items are retained so
     * the counts are deterministic.
     */
    public void testVictimEvictionIntegrity() {
        GoPool pool = new GoPool();
        int n = 3000;
        Set<Object> all = new HashSet<>();
        for (int i = 0; i < n; i++) {
            Object o = new Object();
            all.add(o);
            pool.put(o);
        }
        Set<Object> got = new HashSet<>();
        Object x;
        while ((x = pool.get()) != null) {
            assertTrue(all.contains(x), "foreign item returned: " + x);
            assertTrue(got.add(x), "duplicate item returned: " + x);
        }
        assertTrue(got.size() >= EXPECTED_VICTIM_CAP,
                "eviction kept at least the victim cap: got=" + got.size());
        assertTrue(got.size() <= 1 + 32 + 256 + EXPECTED_VICTIM_CAP,
                "retention bounded by total capacity: got=" + got.size());
    }
}
