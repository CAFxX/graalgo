package graalgo.p3unsafe;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;

/**
 * Concurrency + identity-semantics pins for {@link UnsafeTable}'s scalar
 * normalization cache (I-1, 2026-10-19 review).
 *
 * <ul>
 *   <li>{@code testConcurrentNormalizeSameBox}: two threads race
 *       {@code normalizeScalar} on the same boxes (the
 *       {@code UnsafeOps.openScalar} path, which — unlike {@code intern} —
 *       does not hold the table monitor at the call site). Every box must
 *       normalize to one identical carrier across both threads; a second
 *       carrier would mint a second handle for one box and reintroduce
 *       the P3-M10 identity bug. Many distinct boxes are used so the
 *       first-touch get-then-put race is exercised thousands of times per
 *       run — against the pre-fix unsynchronized code this test fails
 *       (double-minted carriers); against the fix it is stable.</li>
 *   <li>{@code testDistinctEqualBoxesStayDistinct}: two distinct boxes
 *       holding equal values must normalize to <em>distinct</em> carriers.
 *       This pins the identity semantics of the {@code normalized} cache
 *       and is the reason a {@code ConcurrentHashMap} (equals/hashCode
 *       keyed) cannot replace it.</li>
 * </ul>
 *
 * verifies: I-1
 */
public final class UnsafeTableConcurrencyTest {

    /** Boxes per run: each box's first touch is an independent race. */
    private static final int BOXES = 2048;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertSame(Object expected, Object actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected-identical=" + expected + " actual=" + actual);
        }
    }

    /**
     * verifies: I-1 — two threads normalizing the same boxes converge on
     * one carrier per box (identity-stable across threads).
     */
    public void testConcurrentNormalizeSameBox() throws Exception {
        UnsafeTable.install();
        UnsafeTable table = UnsafeTable.current();
        // Out of Integer's cache range: genuinely fresh, distinct boxes.
        Integer[] boxes = new Integer[BOXES];
        for (int i = 0; i < BOXES; i++) {
            boxes[i] = 1_000_000 + i;
        }

        int threads = 2;
        CountDownLatch start = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(threads);
        // carriers[t][i]: every carrier thread t ever saw for boxes[i].
        List<List<Object>> carriers = new ArrayList<>();
        for (int t = 0; t < threads; t++) {
            List<Object> mine = new ArrayList<>(BOXES);
            for (int i = 0; i < BOXES; i++) {
                mine.add(null);
            }
            carriers.add(mine);
        }
        List<Throwable> errors = new ArrayList<>();
        for (int t = 0; t < threads; t++) {
            final int idx = t;
            new Thread(() -> {
                try {
                    start.await();
                    List<Object> mine = carriers.get(idx);
                    // Two passes: the first pass races first-touch on every
                    // box; the second pass re-reads under contention.
                    for (int pass = 0; pass < 2; pass++) {
                        for (int i = 0; i < BOXES; i++) {
                            Object c = table.normalizeScalar(boxes[i]);
                            Object prev = mine.get(i);
                            if (prev == null) {
                                mine.set(i, c);
                            } else if (prev != c) {
                                throw new AssertionError(
                                        "box " + i + " normalized to two carriers across threads");
                            }
                        }
                    }
                } catch (Throwable e) {
                    synchronized (errors) {
                        errors.add(e);
                    }
                } finally {
                    done.countDown();
                }
            }).start();
        }
        start.countDown();
        done.await();
        assertTrue(errors.isEmpty(), "worker threw: " + errors);

        Object first = carriers.get(0).get(0);
        assertTrue(first instanceof int[], "carrier for Integer must be int[1]");
        for (int i = 0; i < BOXES; i++) {
            assertSame(carriers.get(0).get(i), carriers.get(1).get(i),
                    "two threads minted two carriers for box " + i);
        }
        // The table is still coherent afterwards.
        for (int i = 0; i < BOXES; i++) {
            assertSame(carriers.get(0).get(i), table.normalizeScalar(boxes[i]),
                    "post-race normalization diverged for box " + i);
        }
    }

    /**
     * verifies: I-1 — identity (not equality) keys the carrier cache: two
     * distinct boxes with equal values get distinct carriers, hence
     * distinct handles (P3-M10: {@code &x != &y} gc parity).
     */
    public void testDistinctEqualBoxesStayDistinct() {
        UnsafeTable.install();
        UnsafeTable table = UnsafeTable.current();
        Integer a = 1_000_000;
        Integer b = 1_000_000;
        assertTrue(a != b, "test setup: boxes must be distinct objects");
        assertTrue(a.equals(b), "test setup: boxes must hold equal values");

        Object carrierA = table.normalizeScalar(a);
        Object carrierB = table.normalizeScalar(b);
        assertTrue(carrierA != carrierB,
                "equal-valued distinct boxes must not share a carrier "
                        + "(a ConcurrentHashMap-keyed cache would collapse them)");

        long ha = table.intern(a);
        long hb = table.intern(b);
        assertTrue(ha != hb, "distinct boxes must mint distinct handles");
        assertTrue(table.intern(a) == ha, "re-interning the same box must be stable");
    }
}
