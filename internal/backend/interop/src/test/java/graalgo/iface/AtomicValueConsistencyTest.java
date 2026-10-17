package graalgo.iface;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.runtime.Atomics;
import graalgo.runtime.PanicException;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * Java-level tests for the RV-P0-07 {@code atomic.Value} type-consistency
 * protocol in {@link Atomics} ({@code storeGoValue}, {@code swapGoValue},
 * {@code compareAndSwapGoValue}): gc-verbatim panic messages in gc's
 * check order, the empty-cell/first-store rules, typed-nil handling, and
 * the concurrent first-store loser path.
 *
 * <p>Descriptors are real {@link GoTypeDescriptor} instances — the same
 * instance is the same type, distinct instances are different types —
 * exercising the {@code ==} protocol exactly as the runtime uses it.
 * verifies: RV-P0-07
 */
public final class AtomicValueConsistencyTest {

    // gc-verbatim panic messages, in gc's check order (sync/atomic/value.go).
    private static final String STORE_NIL = "sync/atomic: store of nil value into Value";
    private static final String STORE_MISMATCH = "sync/atomic: store of inconsistently typed value into Value";
    private static final String SWAP_NIL = "sync/atomic: swap of nil value into Value";
    private static final String SWAP_MISMATCH = "sync/atomic: swap of inconsistently typed value into Value";
    private static final String CAS_NIL = "sync/atomic: compare and swap of nil value into Value";
    private static final String CAS_OLD_NEW_MISMATCH = "sync/atomic: compare and swap of inconsistently typed values";
    private static final String CAS_STORED_MISMATCH =
            "sync/atomic: compare and swap of inconsistently typed value into Value";

    private final GoTypeDescriptor intDesc = new GoTypeDescriptor("int", GoKind.INT64, false);
    private final GoTypeDescriptor strDesc = new GoTypeDescriptor("string", GoKind.STRING, false);
    private final GoTypeDescriptor ptrDesc = new GoTypeDescriptor("*int", GoKind.POINTER, false);

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(String expected, String actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=<" + expected + "> actual=<" + actual + ">");
        }
    }

    private static void assertNull(Object v, String msg) {
        if (v != null) {
            throw new AssertionError(msg + " expected null, got " + v);
        }
    }

    /** Runs r, asserting it throws PanicException whose Go value is the exact gc string. */
    private static void assertPanicsWith(String expectedMsg, ThrowingRunnable r, String what) {
        String got;
        try {
            r.run();
        } catch (PanicException e) {
            Object v = e.goValue();
            assertTrue(v instanceof GoString,
                    what + ": panic value must be a GoString, got "
                            + (v == null ? "null" : v.getClass().getName()));
            got = ((GoString) v).toString();
            assertTrue(((GoString) v).equals(GoString.fromUtf8(expectedMsg)),
                    what + ": panic message not byte-exact");
            assertEquals(expectedMsg, got, what + ": panic message");
            return;
        } catch (Exception e) {
            throw new AssertionError(what + ": expected PanicException, got " + e);
        }
        throw new AssertionError(what + ": expected panic, none thrown");
    }

    private GoInterface gi(GoTypeDescriptor d, Object data) {
        return new GoInterface(d, data, null);
    }

    private AtomicReference<Object> newCell() {
        return new AtomicReference<>();
    }

    // -- Store ----------------------------------------------------------

    /** Store(nil) panics before touching the cell — even on the first store. */
    public void testStoreNilPanicsOnEmptyCell() {
        assertPanicsWith(STORE_NIL, () -> Atomics.storeGoValue(newCell(), null),
                "Store(nil) as Java null");
        assertPanicsWith(STORE_NIL, () -> Atomics.storeGoValue(newCell(), GoInterface.NIL),
                "Store(nil) as GoInterface.NIL");
    }

    /** Store(nil) panics even when the cell already holds a value (nil check is first). */
    public void testStoreNilPanicsOnFullCell() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        assertPanicsWith(STORE_NIL, () -> Atomics.storeGoValue(cell, null),
                "Store(nil) on full cell");
        // The cell is untouched by the failed store.
        assertTrue(((GoInterface) cell.get()).data().equals(1L), "cell unchanged after Store(nil)");
    }

    /** Same-type stores succeed; the last writer wins (gc's StorePointer tail). */
    public void testStoreSameTypeSucceeds() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        Atomics.storeGoValue(cell, gi(intDesc, 2L));
        GoInterface cur = (GoInterface) cell.get();
        assertTrue(cur.descriptor() == intDesc && cur.data().equals(2L),
                "last same-type store wins, got " + cur);
    }

    /** Store of an inconsistently typed value panics with the exact gc message. */
    public void testStoreInconsistentTypePanics() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        assertPanicsWith(STORE_MISMATCH, () -> Atomics.storeGoValue(cell, gi(strDesc, GoString.fromUtf8("x"))),
                "Store(string) after Store(int)");
        // Failed store leaves the cell untouched.
        assertTrue(((GoInterface) cell.get()).descriptor() == intDesc,
                "cell type word unchanged after panicking store");
    }

    /**
     * A typed nil is storable and fixes the type word (gc: only the nil
     * *interface* panics). Afterwards even a non-nil value of a different
     * type panics.
     */
    public void testStoreTypedNilFixesTypeWord() {
        AtomicReference<Object> cell = newCell();
        // (*int)(nil): non-null descriptor, null data — must NOT panic.
        Atomics.storeGoValue(cell, new GoInterface(ptrDesc, null, null));
        GoInterface cur = (GoInterface) cell.get();
        assertTrue(cur.descriptor() == ptrDesc && cur.data() == null,
                "typed nil stored with its type word, got " + cur);
        // Storing an int now panics: the cell's type word is *int.
        assertPanicsWith(STORE_MISMATCH, () -> Atomics.storeGoValue(cell, gi(intDesc, 1L)),
                "Store(int) after Store((*int)(nil))");
        // A second typed nil of the same type is fine.
        Atomics.storeGoValue(cell, new GoInterface(ptrDesc, null, null));
    }

    // -- Swap -----------------------------------------------------------

    /** Swap(nil) panics before touching the cell — even on the first store. */
    public void testSwapNilPanics() {
        assertPanicsWith(SWAP_NIL, () -> Atomics.swapGoValue(newCell(), null),
                "Swap(nil) on empty cell");
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        assertPanicsWith(SWAP_NIL, () -> Atomics.swapGoValue(cell, GoInterface.NIL),
                "Swap(nil) on full cell");
    }

    /** Swap on an empty cell is the first store and returns null (gc: old is nil). */
    public void testSwapOnEmptyCellReturnsNull() {
        AtomicReference<Object> cell = newCell();
        Object prev = Atomics.swapGoValue(cell, gi(intDesc, 7L));
        assertNull(prev, "first Swap must return null");
        GoInterface cur = (GoInterface) cell.get();
        assertTrue(cur.descriptor() == intDesc && cur.data().equals(7L),
                "first Swap stored the value, got " + cur);
    }

    /** Swap of the same type returns the previous GoInterface and installs the new one. */
    public void testSwapSameTypeReturnsOld() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        Object prev = Atomics.swapGoValue(cell, gi(intDesc, 2L));
        assertTrue(prev instanceof GoInterface gi && gi.descriptor() == intDesc && gi.data().equals(1L),
                "Swap must return the previous value, got " + prev);
        assertTrue(((GoInterface) cell.get()).data().equals(2L), "Swap must install the new value");
    }

    /** Swap of an inconsistently typed value panics with the exact gc message. */
    public void testSwapInconsistentTypePanics() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        assertPanicsWith(SWAP_MISMATCH, () -> Atomics.swapGoValue(cell, gi(strDesc, GoString.fromUtf8("x"))),
                "Swap(string) after Store(int)");
        assertTrue(((GoInterface) cell.get()).data().equals(1L), "cell unchanged after panicking swap");
    }

    // -- CompareAndSwap -------------------------------------------------

    /**
     * CAS(old, nil) panics first — even on an empty cell, before any
     * type comparison or cell access.
     */
    public void testCasNilNewPanicsFirst() {
        assertPanicsWith(CAS_NIL, () -> Atomics.compareAndSwapGoValue(newCell(), GoInterface.NIL, null),
                "CAS(x, nil) on empty cell");
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        assertPanicsWith(CAS_NIL, () -> Atomics.compareAndSwapGoValue(cell, gi(intDesc, 1L), GoInterface.NIL),
                "CAS(x, nil) on full cell");
    }

    /**
     * CAS with non-nil old whose type differs from new's panics with the
     * plural "values" message — before cell access (so even on an empty cell).
     */
    public void testCasOldNewTypeMismatchPanicsOnEmptyCell() {
        assertPanicsWith(CAS_OLD_NEW_MISMATCH,
                () -> Atomics.compareAndSwapGoValue(newCell(), gi(intDesc, 1L), gi(strDesc, GoString.fromUtf8("x"))),
                "CAS(int, string) on empty cell");
    }

    /** The old/new check fires before the stored-type check (gc order). */
    public void testCasOldNewCheckBeforeStoredTypeCheck() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(strDesc, GoString.fromUtf8("s")));
        // old=int/new=string would panic on stored type too, but the
        // old/new (plural) panic must come first.
        assertPanicsWith(CAS_OLD_NEW_MISMATCH,
                () -> Atomics.compareAndSwapGoValue(cell, gi(intDesc, 1L), gi(strDesc, GoString.fromUtf8("x"))),
                "CAS old/new check precedes stored-type check");
    }

    /** CAS where the stored type differs from new's type panics (singular "value"). */
    public void testCasStoredTypeMismatchPanics() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 1L));
        assertPanicsWith(CAS_STORED_MISMATCH,
                () -> Atomics.compareAndSwapGoValue(cell, gi(strDesc, GoString.fromUtf8("a")),
                        gi(strDesc, GoString.fromUtf8("b"))),
                "CAS(string, string) on int cell");
        assertTrue(((GoInterface) cell.get()).data().equals(1L), "cell unchanged after panicking CAS");
    }

    /** CAS on an empty cell: non-nil old returns false without storing. */
    public void testCasEmptyCellNonNilOldReturnsFalse() {
        AtomicReference<Object> cell = newCell();
        assertTrue(!Atomics.compareAndSwapGoValue(cell, gi(intDesc, 1L), gi(intDesc, 2L)),
                "CAS on empty cell with non-nil old must return false");
        assertNull(cell.get(), "failed CAS must not store");
    }

    /** CAS on an empty cell: nil old performs the first store and returns true. */
    public void testCasEmptyCellNilOldStoresAndReturnsTrue() {
        AtomicReference<Object> cell = newCell();
        assertTrue(Atomics.compareAndSwapGoValue(cell, null, gi(intDesc, 9L)),
                "CAS(nil, v) on empty cell must return true");
        GoInterface cur = (GoInterface) cell.get();
        assertTrue(cur.descriptor() == intDesc && cur.data().equals(9L),
                "CAS(nil, v) must perform the first store, got " + cur);
        // And now the cell has a type word: a different-typed CAS panics.
        assertPanicsWith(CAS_STORED_MISMATCH,
                () -> Atomics.compareAndSwapGoValue(cell, GoInterface.NIL, gi(strDesc, GoString.fromUtf8("x"))),
                "CAS(nil, string) after first-store int");
    }

    /** Equal values (same type, Go ==) swap and return true — separately boxed values too. */
    public void testCasEqualValuesReturnsTrue() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 5L));
        // old boxed separately from the stored value: identity differs,
        // Go == holds — this is why the loop uses GoEquality, not ==.
        assertTrue(Atomics.compareAndSwapGoValue(cell, gi(intDesc, 5L), gi(intDesc, 6L)),
                "CAS with equal old must return true");
        assertTrue(((GoInterface) cell.get()).data().equals(6L), "CAS must install new value");
        // Strings compare by value as well.
        AtomicReference<Object> scell = newCell();
        Atomics.storeGoValue(scell, gi(strDesc, GoString.fromUtf8("abc")));
        assertTrue(Atomics.compareAndSwapGoValue(scell, gi(strDesc, GoString.fromUtf8("abc")),
                gi(strDesc, GoString.fromUtf8("abd"))),
                "CAS with equal GoString old must return true");
    }

    /** Unequal old returns false and leaves the cell alone. */
    public void testCasUnequalOldReturnsFalse() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 5L));
        assertTrue(!Atomics.compareAndSwapGoValue(cell, gi(intDesc, 4L), gi(intDesc, 6L)),
                "CAS with unequal old must return false");
        assertTrue(((GoInterface) cell.get()).data().equals(5L), "failed CAS must not modify the cell");
    }

    /** Nil old against a non-empty cell: types agree, values don't — false, no panic. */
    public void testCasNilOldOnFullCellReturnsFalse() {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 5L));
        assertTrue(!Atomics.compareAndSwapGoValue(cell, null, gi(intDesc, 6L)),
                "CAS(nil, v) on non-empty cell must return false");
        assertTrue(((GoInterface) cell.get()).data().equals(5L), "cell unchanged");
    }

    // -- Load -----------------------------------------------------------

    /** Load on a zero Value yields nil (Java null flows as the nil interface). */
    public void testLoadOnEmptyCellIsNil() {
        assertNull(Atomics.loadPointer(newCell()), "Load of a zero Value must be nil");
    }

    // -- Concurrency ----------------------------------------------------

    /**
     * Concurrent first stores of *different* types: exactly one CAS wins;
     * the loser re-reads the winner's type word and panics (the gc
     * spin-then-check outcome, without the sentinel). Over many fresh
     * cells, exactly one thread panics per round and the cell always ends
     * with a single coherent type — never a torn state.
     */
    public void testConcurrentFirstStoreTwoTypesExactlyOnePanics() throws Exception {
        int rounds = 200;
        for (int r = 0; r < rounds; r++) {
            AtomicReference<Object> cell = newCell();
            CountDownLatch start = new CountDownLatch(1);
            int[] panics = new int[2];
            Thread[] ts = new Thread[2];
            GoInterface[] vals = {gi(intDesc, (long) r), gi(strDesc, GoString.fromUtf8("r" + r))};
            for (int t = 0; t < 2; t++) {
                final int ti = t;
                ts[t] = new Thread(() -> {
                    try {
                        start.await();
                        Atomics.storeGoValue(cell, vals[ti]);
                    } catch (PanicException e) {
                        panics[ti]++;
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                    }
                });
                ts[t].start();
            }
            start.countDown();
            ts[0].join();
            ts[1].join();
            assertTrue(panics[0] + panics[1] == 1,
                    "round " + r + ": exactly one loser must panic, panics=" + panics[0] + "," + panics[1]);
            GoInterface cur = (GoInterface) cell.get();
            assertTrue(cur.descriptor() == intDesc || cur.descriptor() == strDesc,
                    "round " + r + ": cell must hold one coherent type, got " + cur);
        }
    }

    /**
     * Concurrent same-type first stores: both succeed, no panic, the cell
     * holds the type with one of the two values (last-writer-wins).
     */
    public void testConcurrentFirstStoreSameTypeNoPanic() throws Exception {
        int rounds = 200;
        for (int r = 0; r < rounds; r++) {
            AtomicReference<Object> cell = newCell();
            CountDownLatch start = new CountDownLatch(1);
            boolean[] panicked = new boolean[1];
            Thread t1 = new Thread(() -> {
                try {
                    start.await();
                    Atomics.storeGoValue(cell, gi(intDesc, 1L));
                } catch (PanicException e) {
                    panicked[0] = true;
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
            });
            Thread t2 = new Thread(() -> {
                try {
                    start.await();
                    Atomics.storeGoValue(cell, gi(intDesc, 2L));
                } catch (PanicException e) {
                    panicked[0] = true;
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
            });
            t1.start();
            t2.start();
            start.countDown();
            t1.join();
            t2.join();
            assertTrue(!panicked[0], "round " + r + ": same-type concurrent stores must not panic");
            GoInterface cur = (GoInterface) cell.get();
            assertTrue(cur.descriptor() == intDesc
                            && (cur.data().equals(1L) || cur.data().equals(2L)),
                    "round " + r + ": cell holds one of the stored values, got " + cur);
        }
    }

    /**
     * Same-type hammer across Store/Swap/CAS: never panics; the cell
     * always holds a coherent int value (linearizability smoke test).
     */
    public void testConcurrentSameTypeHammer() throws Exception {
        AtomicReference<Object> cell = newCell();
        Atomics.storeGoValue(cell, gi(intDesc, 0L));
        int threads = 8, perThread = 500;
        CountDownLatch start = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(threads);
        boolean[] panicked = new boolean[1];
        for (int t = 0; t < threads; t++) {
            final int base = t * perThread;
            new Thread(() -> {
                try {
                    start.await();
                    for (int i = 0; i < perThread; i++) {
                        long v = base + i;
                        Atomics.storeGoValue(cell, gi(intDesc, v));
                        Atomics.swapGoValue(cell, gi(intDesc, v + 1));
                        Atomics.compareAndSwapGoValue(cell, gi(intDesc, v + 1), gi(intDesc, v + 2));
                    }
                } catch (PanicException e) {
                    panicked[0] = true;
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                } finally {
                    done.countDown();
                }
            }).start();
        }
        start.countDown();
        done.await();
        assertTrue(!panicked[0], "same-type hammer must never panic");
        GoInterface cur = (GoInterface) cell.get();
        assertTrue(cur.descriptor() == intDesc && cur.data() instanceof Long,
                "cell holds a coherent int value, got " + cur);
    }
}
