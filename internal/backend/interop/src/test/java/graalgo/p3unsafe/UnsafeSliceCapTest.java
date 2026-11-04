package graalgo.p3unsafe;

import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * R11-UNSAFE (REVIEW-2026-11-04-11 NEW-P0-10): unit tests for
 * {@link UnsafeOps#slice} — gc's {@code unsafe.Slice} yields
 * {@code len == cap == len} by construction, never the backing array's
 * remainder or the source slice's cap.
 *
 * <p>gc 1.27.1 oracle (probe 2026-11-04): {@code Slice(&a[0], 2)} over a
 * {@code [4]int} gives len 2 cap 2; {@code Slice(&a[1], 2)} gives len 2
 * cap 2; {@code Slice(&a[0], 10)} over a {@code [4]int} builds WITHOUT
 * panicking (len 10 cap 10) — gc does not check len against the backing
 * size. Nil pointer with len &gt; 0 panics
 * ("ptr is nil and len is not zero"); negative len panics
 * ("len out of range").
 *
 * <p>What the old code got wrong: it returned the remainder / source
 * cap, so {@code cap()} differed from gc and {@code append} past
 * {@code len} wrote into the adjacent backing array instead of
 * reallocating. These tests fail on the unfixed backend (cap tests)
 * or pin the corrected panic boundary (len within the physical array,
 * even past the source slice's cap, is representable and allowed; len
 * past the physical array is the intentional divergence pinned by
 * corpus README item 10 — {@code GoSlice} cannot represent elements
 * past its backing array, and growing/copying would break aliasing).
 */
public final class UnsafeSliceCapTest {

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

    /** A fresh table per test: handles restart from 1, like a new run. */
    private static void freshTable() {
        UnsafeTable.install();
    }

    public void testCapEqualsLenWhenLenBelowRemainder() {
        freshTable();
        long[] backing = new long[4];
        GoSlice s = UnsafeOps.slice(new UnsafeTable.ArrayRef(backing, 0), 2, GoKind.INT64, null);
        assertEquals(2, s.len(), "len must be the requested len");
        assertEquals(2, s.cap(), "cap must equal len, not the backing remainder (4)");
        assertTrue(s.array() == backing, "result must alias the backing array");
    }

    public void testCapEqualsLenFromOffsetPointer() {
        freshTable();
        long[] backing = new long[4];
        GoSlice s = UnsafeOps.slice(new UnsafeTable.ArrayRef(backing, 1), 2, GoKind.INT64, null);
        assertEquals(2, s.len(), "len must be the requested len");
        assertEquals(2, s.cap(), "cap must equal len, not the remainder from the offset (3)");
        assertEquals(1, s.offset(), "offset must be preserved");
    }

    public void testCapEqualsLenFromGoSliceReferent() {
        freshTable();
        long[] backing = new long[4];
        GoSlice src = new GoSlice(backing, 0, 2, 4);
        GoSlice s = UnsafeOps.slice(src, 2, GoKind.INT64, null);
        assertEquals(2, s.len(), "len must be the requested len");
        assertEquals(2, s.cap(), "cap must equal len, not the source slice's cap (4)");
    }

    public void testZeroLenCapIsZero() {
        freshTable();
        long[] backing = new long[4];
        GoSlice s = UnsafeOps.slice(new UnsafeTable.ArrayRef(backing, 0), 0, GoKind.INT64, null);
        assertEquals(0, s.len(), "len must be 0");
        assertEquals(0, s.cap(), "cap must be 0, not the backing remainder (4)");
    }

    public void testLenWithinPhysicalArrayPastSourceCapAllowed() {
        freshTable();
        // gc does not check len against the source slice's cap: a view
        // reaching elements 2..3 of the physical array is representable.
        long[] backing = new long[4];
        GoSlice src = new GoSlice(backing, 0, 2, 2);
        GoSlice s = UnsafeOps.slice(src, 4, GoKind.INT64, null);
        assertEquals(4, s.len(), "len must be the requested len");
        assertEquals(4, s.cap(), "cap must equal len");
        assertTrue(s.array() == backing, "result must alias the backing array");
    }

    public void testOverLongLenPastPhysicalArrayPanics() {
        freshTable();
        long[] backing = new long[4];
        try {
            UnsafeOps.slice(new UnsafeTable.ArrayRef(backing, 0), 5, GoKind.INT64, null);
        } catch (PanicException e) {
            return; // intentional divergence: unrepresentable in the model
        }
        throw new AssertionError("expected PanicException for len past the physical array");
    }

    public void testNilPointerLenZeroIsNil() {
        freshTable();
        assertTrue(UnsafeOps.slice(null, 0, GoKind.INT64, null) == null,
                "nil pointer with len 0 must yield the nil slice");
    }

    public void testNilPointerLenNonzeroPanics() {
        freshTable();
        try {
            UnsafeOps.slice(null, 1, GoKind.INT64, null);
        } catch (PanicException e) {
            return; // gc: "ptr is nil and len is not zero"
        }
        throw new AssertionError("expected PanicException for nil pointer with len != 0");
    }

    public void testNegativeLenPanics() {
        freshTable();
        long[] backing = new long[4];
        try {
            UnsafeOps.slice(new UnsafeTable.ArrayRef(backing, 0), -1, GoKind.INT64, null);
        } catch (PanicException e) {
            return; // gc: "len out of range"
        }
        throw new AssertionError("expected PanicException for negative len");
    }
}
