package graalgo.values;

import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.runtime.GoFatalError;

/**
 * Review 2026-11-05, B-P1-4: {@code GoMap} concurrency-guard tests.
 *
 * <p>Deterministic core: the guard is held directly via the
 * package-private {@code beginWrite}/{@code endWrite} and
 * {@code beginIteration}/{@code endIteration}; the same-thread CAS
 * self-fatal proves every public op is actually wired to the guard (a
 * guard that exists but is never consulted would pass unit tests of the
 * primitives alone — the wiring tests close that hole). Stress tests
 * cover the concurrent paths.
 *
 * <p>Keys are {@link GoString} (Go string values): {@code GoMap} keys use
 * {@link GoEquality} (Go {@code ==} semantics) — a {@code java.lang.String}
 * is not a valid key (identity hash).
 *
 * <p>verifies: review 2026-11-05 B-P1-4 (concurrent map access fails
 * loudly with gc's exact messages, never corrupts silently)
 */
public final class GoMapGuardTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    /** Go string key. */
    private static GoString k(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    private static void assertFatalMessage(String want, Runnable r, String what) {
        try {
            r.run();
        } catch (GoFatalError f) {
            assertTrue(want.equals(f.getMessage()),
                    what + ": wrong message want=[" + want + "] got=[" + f.getMessage() + "]");
            return;
        }
        throw new AssertionError(what + ": expected GoFatalError, none thrown");
    }

    public void testDoubleBeginWriteFatals() {
        GoMap m = new GoMap();
        m.beginWrite();
        try {
            assertFatalMessage("concurrent map writes", m::beginWrite, "double beginWrite");
        } finally {
            m.endWrite();
        }
    }

    public void testCheckReadWhileHeldFatals() {
        GoMap m = new GoMap();
        m.beginWrite();
        try {
            assertFatalMessage("concurrent map read and map write", m::checkRead, "checkRead");
        } finally {
            m.endWrite();
        }
    }

    public void testCheckIterationWhileHeldFatals() {
        GoMap m = new GoMap();
        m.beginWrite();
        try {
            assertFatalMessage("concurrent map iteration and map write",
                    m::checkIteration, "checkIteration");
        } finally {
            m.endWrite();
        }
    }

    /** Every public mutating op is wired to the write guard. */
    public void testWiringWritesFatalWhileHeld() {
        GoMap m = new GoMap();
        m.beginWrite();
        try {
            assertFatalMessage("concurrent map writes", () -> m.put(k("k"), 1), "put");
            assertFatalMessage("concurrent map writes", () -> m.delete(k("k")), "delete");
            assertFatalMessage("concurrent map writes", m::clear, "clear");
        } finally {
            m.endWrite();
        }
    }

    /** Every public read op is wired to the read/iteration guard. */
    public void testWiringReadsFatalWhileHeld() {
        GoMap m = new GoMap();
        m.put(k("k"), 1);
        m.beginWrite();
        try {
            assertFatalMessage("concurrent map read and map write",
                    () -> m.get(k("k")), "get");
            assertFatalMessage("concurrent map read and map write",
                    () -> m.containsKey(k("k")), "containsKey");
            assertFatalMessage("concurrent map iteration and map write",
                    m::keysSnapshot, "keysSnapshot");
        } finally {
            m.endWrite();
        }
    }

    /**
     * {@code beginIterationChecked} increments before checking: with a
     * write in progress it throws the iteration message AND releases
     * the count (a failed snapshot must not strand it nonzero — a
     * subsequent put works instead of self-fataling). Fails on the old
     * check-then-increment order only in the timing window, so this pins
     * the released-count half deterministically; the ordering itself is
     * closed by construction (increment precedes the check).
     */
    public void testBeginIterationCheckedWhileHeldFatalsAndReleases() {
        GoMap m = new GoMap();
        m.put(k("k"), 1);
        m.beginWrite();
        try {
            assertFatalMessage("concurrent map iteration and map write",
                    m::beginIterationChecked, "beginIterationChecked while writing");
        } finally {
            m.endWrite();
        }
        // The failed acquisition released the iteration count: a write
        // now proceeds instead of fataling on a stranded count.
        m.put(k("x"), 2);
        assertTrue(Integer.valueOf(2).equals(m.get(k("x"))), "put after failed beginIterationChecked");
    }

    /** A write racing an in-progress iteration fatals with the iteration message. */
    public void testWiringIterationBlocksWrite() {
        GoMap m = new GoMap();
        m.put(k("k"), 1);
        m.beginIteration();
        try {
            assertFatalMessage("concurrent map iteration and map write",
                    () -> m.put(k("x"), 2), "put during iteration");
            assertFatalMessage("concurrent map iteration and map write",
                    () -> m.delete(k("k")), "delete during iteration");
            assertFatalMessage("concurrent map iteration and map write",
                    m::clear, "clear during iteration");
        } finally {
            m.endIteration();
        }
        // The failed acquisition must have released the write guard:
        // a subsequent put works instead of self-fataling.
        m.put(k("x"), 2);
        assertTrue(Integer.valueOf(2).equals(m.get(k("x"))), "put after failed iteration-guarded write");
    }

    /** A snapshot taken via the checked entry point blocks a racing write. */
    public void testBeginIterationCheckedBlocksWrite() {
        GoMap m = new GoMap();
        m.put(k("k"), 1);
        m.beginIterationChecked();
        try {
            assertFatalMessage("concurrent map iteration and map write",
                    () -> m.put(k("x"), 2), "put during checked iteration");
        } finally {
            m.endIteration();
        }
        m.put(k("x"), 2);
        assertTrue(Integer.valueOf(2).equals(m.get(k("x"))), "put after checked iteration ends");
    }

    /** Single-threaded interleaved read/write sequences never trip the guard. */
    public void testInterleavedSingleThreadedOps() {
        GoMap m = new GoMap();
        for (int round = 0; round < 100; round++) {
            // The range-over-map pattern: snapshot, read per key, write
            // from the loop body, snapshot again.
            Object[] keys = m.keysSnapshot();
            for (Object key : keys) {
                m.get(key);
            }
            m.put(k("r" + round), round);
            if (round % 2 == 0) {
                m.delete(k("r" + (round / 2)));
            }
        }
        assertTrue(m.size() > 0, "interleaved ops leave entries");
    }

    public void testNormalOperationAfterRelease() {
        GoMap m = new GoMap();
        m.beginWrite();
        m.endWrite();
        m.put(k("a"), 1);
        m.put(k("b"), 2);
        assertTrue(Integer.valueOf(1).equals(m.get(k("a"))), "get after release");
        assertTrue(m.containsKey(k("b")), "containsKey after release");
        assertTrue(m.keysSnapshot().length == 2, "keysSnapshot after release");
        m.delete(k("a"));
        assertTrue(m.size() == 1, "size after delete");
        // Resize path: 200 distinct keys force several doublings; every
        // key must survive rehashing.
        for (int i = 0; i < 200; i++) {
            m.put(k("n" + i), i);
        }
        assertTrue(m.size() == 201, "size after resize-path puts");
        assertTrue(m.keysSnapshot().length == 201, "snapshot after resize-path puts");
        for (int i = 0; i < 200; i++) {
            assertTrue(Integer.valueOf(i).equals(m.get(k("n" + i))), "get after resize " + i);
        }
        m.clear();
        assertTrue(m.size() == 0, "size after clear");
        assertTrue(m.keysSnapshot().length == 0, "snapshot after clear");
    }

    /**
     * Real concurrent writes: either a writer observes the loud fatal
     * (gc parity), or the run serialized and the map is provably intact.
     * Both branches are asserted, so the test is never vacuous and fails
     * on unguarded code whenever the writes interleave and corrupt.
     */
    public void testConcurrentWritesFatalOrSafe() throws Exception {
        int perThread = 2000;
        for (int trial = 0; trial < 5; trial++) {
            GoMap m = new GoMap();
            CountDownLatch go = new CountDownLatch(1);
            CountDownLatch done = new CountDownLatch(2);
            AtomicReference<Throwable> fatal = new AtomicReference<>();
            for (int w = 0; w < 2; w++) {
                final int worker = w;
                new Thread(() -> {
                    try {
                        go.await();
                        for (int i = 0; i < perThread; i++) {
                            m.put(k("w" + worker + "-" + i), i);
                        }
                    } catch (GoFatalError f) {
                        fatal.compareAndSet(null, f);
                    } catch (Throwable th) {
                        fatal.compareAndSet(null, th);
                    } finally {
                        done.countDown();
                    }
                }).start();
            }
            go.countDown();
            done.await();
            Throwable f = fatal.get();
            if (f != null) {
                assertTrue(f instanceof GoFatalError,
                        "trial " + trial + ": concurrent put threw non-fatal " + f);
                assertTrue("concurrent map writes".equals(f.getMessage()),
                        "trial " + trial + ": wrong fatal message [" + f.getMessage() + "]");
            } else {
                assertTrue(m.size() == 2 * perThread,
                        "trial " + trial + ": size=" + m.size() + " want=" + (2 * perThread));
                Object[] keys = m.keysSnapshot();
                assertTrue(keys.length == 2 * perThread,
                        "trial " + trial + ": snapshot=" + keys.length + " want=" + (2 * perThread));
                for (Object key : keys) {
                    assertTrue(key != null, "trial " + trial + ": null key in snapshot");
                }
                for (int w = 0; w < 2; w++) {
                    for (int i = 0; i < perThread; i++) {
                        assertTrue(m.get(k("w" + w + "-" + i)) != null,
                                "trial " + trial + ": lost entry w" + w + "-" + i);
                    }
                }
            }
        }
    }

    /**
     * Concurrent read + write: either the loud fatal fires, or every
     * snapshot the reader took was intact (no null keys, no torn read).
     * On unguarded code the reader can hit the snapshot-sizing
     * {@code ArrayIndexOutOfBoundsException} — which fails this test.
     */
    public void testConcurrentReadWriteFatalOrSafe() throws Exception {
        GoMap m = new GoMap();
        for (int i = 0; i < 500; i++) {
            m.put(k("seed" + i), i);
        }
        CountDownLatch go = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(2);
        AtomicReference<Throwable> seen = new AtomicReference<>();
        new Thread(() -> {
            try {
                go.await();
                for (int i = 0; i < 2000; i++) {
                    m.put(k("w" + i), i);
                }
            } catch (Throwable th) {
                seen.compareAndSet(null, th);
            } finally {
                done.countDown();
            }
        }).start();
        new Thread(() -> {
            try {
                go.await();
                for (int i = 0; i < 2000; i++) {
                    Object[] keys = m.keysSnapshot();
                    for (Object key : keys) {
                        if (key == null) {
                            throw new AssertionError("null key in snapshot");
                        }
                    }
                    m.get(k("seed0"));
                }
            } catch (Throwable th) {
                seen.compareAndSet(null, th);
            } finally {
                done.countDown();
            }
        }).start();
        go.countDown();
        done.await();
        Throwable t = seen.get();
        if (t != null) {
            assertTrue(t instanceof GoFatalError,
                    "read/write race threw non-fatal " + t);
            String msg = t.getMessage();
            assertTrue("concurrent map writes".equals(msg)
                            || "concurrent map read and map write".equals(msg)
                            || "concurrent map iteration and map write".equals(msg),
                    "read/write race wrong fatal message [" + msg + "]");
        }
    }
}
