package graalgo.values;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.CountDownLatch;

/**
 * Concurrency tests for {@link GoMap} (review P0-2, 2026-10-30).
 *
 * <p>Goroutines are real virtual threads, so one Go map value can be
 * written from several goroutines at once. Every method on GoMap must
 * therefore be atomic with respect to every other method on the same
 * map: concurrent writers must neither lose entries nor corrupt probe
 * chains, and concurrent readers must never observe a torn table
 * (mixed-generation arrays, a snapshot longer than the map, a wrong
 * value for a key).
 *
 * <p>Each test is deterministic in its final state: threads own disjoint
 * key ranges, so the expected size and contents are exact. A failure is
 * a real corruption (lost entry, wrong value, exception), not flakiness
 * in the expectation.
 */
public final class GoMapConcurrencyTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static void assertEq(Object want, Object got, String what) {
        if (want == null ? got != null : !want.equals(got)) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    /** Runs all threads to completion, rethrowing the first failure. */
    private static void joinAll(List<Thread> threads, ConcurrentLinkedQueue<Throwable> errors)
            throws InterruptedException {
        for (Thread t : threads) {
            t.join();
        }
        Throwable first = errors.peek();
        if (first != null) {
            throw new AssertionError("worker failed: " + first, first);
        }
    }

    private static Thread vthread(Runnable body, ConcurrentLinkedQueue<Throwable> errors) {
        return Thread.ofVirtual().unstarted(() -> {
            try {
                body.run();
            } catch (Throwable t) {
                errors.add(t);
            }
        });
    }

    /**
     * 16 writers insert disjoint keys into one map starting at capacity 8,
     * so the table resizes repeatedly under contention. Final state must
     * be exact: every key present with its own value, size exact.
     */
    public void testConcurrentPutDistinctKeys() throws InterruptedException {
        for (int round = 0; round < 3; round++) {
            GoMap m = new GoMap();
            int threads = 16;
            int perThread = 1000;
            CountDownLatch go = new CountDownLatch(1);
            List<Thread> workers = new ArrayList<>();
            ConcurrentLinkedQueue<Throwable> errors = new ConcurrentLinkedQueue<>();
            for (int t = 0; t < threads; t++) {
                final long base = (long) t * perThread;
                workers.add(vthread(() -> {
                    try {
                        go.await();
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                        throw new RuntimeException(e);
                    }
                    for (long k = base; k < base + perThread; k++) {
                        m.put(k, k);
                    }
                }, errors));
            }
            for (Thread w : workers) {
                w.start();
            }
            go.countDown();
            joinAll(workers, errors);
            assertEq((long) threads * perThread, m.size(), "round " + round + " size");
            for (long k = 0; k < (long) threads * perThread; k++) {
                assertEq(k, m.get(k), "round " + round + " key " + k);
            }
        }
    }

    /**
     * Writers, deleters and readers run together. Writers own disjoint
     * ranges; deleters remove exactly their own range after inserting it;
     * readers assert every observed value is either absent or exactly the
     * value written for that key (never a torn/wrong value), and every
     * snapshot is well-formed (no nulls, no duplicates, length equal to
     * the size it was taken from is checked at quiescence).
     */
    public void testConcurrentPutDeleteRead() throws InterruptedException {
        for (int round = 0; round < 3; round++) {
            GoMap m = new GoMap();
            int writers = 8;
            int perThread = 500;
            CountDownLatch go = new CountDownLatch(1);
            List<Thread> workers = new ArrayList<>();
            ConcurrentLinkedQueue<Throwable> errors = new ConcurrentLinkedQueue<>();
            for (int t = 0; t < writers; t++) {
                final long base = (long) t * perThread;
                final boolean deleteAfter = (t % 2 == 0);
                workers.add(vthread(() -> {
                    try {
                        go.await();
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                        throw new RuntimeException(e);
                    }
                    for (long k = base; k < base + perThread; k++) {
                        m.put(k, k);
                    }
                    if (deleteAfter) {
                        for (long k = base; k < base + perThread; k++) {
                            m.delete(k);
                        }
                    }
                }, errors));
            }
            // Readers: only assert values they can prove (absent, or the
            // key's own value); any other outcome is a torn read.
            for (int r = 0; r < 4; r++) {
                workers.add(vthread(() -> {
                    try {
                        go.await();
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                        throw new RuntimeException(e);
                    }
                    for (int i = 0; i < 20000; i++) {
                        long k = (i * 7919L) % (writers * perThread);
                        Object v = m.get(k);
                        if (v != null && !v.equals(k)) {
                            throw new AssertionError("torn read: key " + k + " -> " + v);
                        }
                        Object[] snap = m.keysSnapshot();
                        Set<Object> seen = new HashSet<>();
                        for (Object key : snap) {
                            if (key == null) {
                                throw new AssertionError("null entry in snapshot");
                            }
                            if (!seen.add(key)) {
                                throw new AssertionError("duplicate key in snapshot: " + key);
                            }
                        }
                    }
                }, errors));
            }
            for (Thread w : workers) {
                w.start();
            }
            go.countDown();
            joinAll(workers, errors);
            // Odd threads kept their ranges: 4 threads x 500 keys.
            assertEq(4L * perThread, m.size(), "round " + round + " final size");
            Object[] snap = m.keysSnapshot();
            assertEq(m.size(), snap.length, "round " + round + " snapshot length");
            for (int t = 1; t < writers; t += 2) {
                long base = (long) t * perThread;
                for (long k = base; k < base + perThread; k++) {
                    assertEq(k, m.get(k), "round " + round + " surviving key " + k);
                }
            }
            for (int t = 0; t < writers; t += 2) {
                long base = (long) t * perThread;
                for (long k = base; k < base + perThread; k++) {
                    assertTrue(!m.containsKey(k), "round " + round + " deleted key " + k);
                }
            }
        }
    }

    /**
     * A reader iterating (snapshot + per-key re-check, the range-over-map
     * pattern) while writers insert must never throw and must never see
     * a key whose value is wrong. Single-threaded range semantics are
     * unchanged; this only pins the concurrent reader's safety.
     */
    public void testConcurrentRangeDuringWrite() throws InterruptedException {
        GoMap m = new GoMap();
        int writers = 8;
        int perThread = 1000;
        CountDownLatch go = new CountDownLatch(1);
        List<Thread> workers = new ArrayList<>();
        ConcurrentLinkedQueue<Throwable> errors = new ConcurrentLinkedQueue<>();
        for (int t = 0; t < writers; t++) {
            final long base = (long) t * perThread;
            workers.add(vthread(() -> {
                try {
                    go.await();
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                    throw new RuntimeException(e);
                }
                for (long k = base; k < base + perThread; k++) {
                    m.put(k, k);
                }
            }, errors));
        }
        workers.add(vthread(() -> {
            try {
                go.await();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new RuntimeException(e);
            }
            for (int i = 0; i < 500; i++) {
                for (Object key : m.keysSnapshot()) {
                    Object v = m.get(key);
                    if (v != null && !v.equals(key)) {
                        throw new AssertionError("range saw torn value: key " + key + " -> " + v);
                    }
                }
            }
        }, errors));
        for (Thread w : workers) {
            w.start();
        }
        go.countDown();
        joinAll(workers, errors);
        assertEq((long) writers * perThread, m.size(), "final size");
    }
}
