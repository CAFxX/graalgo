package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

/**
 * Regression tests for R10-02 (review-2026-11-09-10): the v2
 * {@link ClearNode} threw a Java {@link NullPointerException} on a nil
 * slice (the comment claimed a no-op), silently never cleared
 * {@code []float32} (no {@code float[]} branch), nulled complex slots
 * (whose zero is the {@code ZERO} value, not null), and silently did
 * nothing for an unexpected operand kind.
 *
 * <p>RED (pre-fix): {@code testNilSliceIsNoOp} fails with
 * {@code NullPointerException} at {@code gs.array()};
 * {@code testFloat32SliceCleared} fails because elements 1..2 keep their
 * values; {@code testComplex64SliceCleared} /
 * {@code testComplex128SliceCleared} fail because the slots become
 * {@code null} instead of {@code ZERO}; {@code testUnexpectedKindThrows}
 * fails because the node silently returns 0;
 * {@code testExecuteObjectClearsAndReturnsNull} fails with
 * {@code UnsupportedOperationException} (no executeObject override).
 * GREEN: nil is a no-op, every D5 backing-array type is zeroed over
 * exactly {@code [offset, offset+len)}, and an unexpected kind fails fast.
 *
 * <p>Positive controls ({@code testNilMapIsNoOp},
 * {@code testFloat64SliceCleared}, {@code testIntSliceCleared},
 * {@code testByteSliceCleared}, {@code testObjectSliceCleared},
 * {@code testMapCleared}, {@code testEmptySliceIsNoOp}) were green before
 * the fix and pin the behaviour the refactor must not change.
 */
public final class ClearNodeTest {

    /** A child node returning a fixed value of a fixed kind. */
    private static final class ConstNode extends GoExprNode {
        private final Object value;

        ConstNode(GoKind kind, Object value) {
            super(kind);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    private static long clear(GoKind kind, Object value) {
        return new ClearNode(new ConstNode(kind, value)).executeLong(null);
    }

    private static void assertTrue(boolean cond, String message) {
        if (!cond) {
            throw new AssertionError(message);
        }
    }

    private static void assertZero(long actual, String message) {
        assertTrue(actual == 0L, message + " (got " + actual + ")");
    }

    /** clear of a nil slice is a gc no-op, not a Java NPE (R10-02). */
    public void testNilSliceIsNoOp() {
        assertZero(clear(GoKind.SLICE, null), "clear(nil slice) must return 0");
    }

    /** clear of a nil map is a gc no-op (positive control). */
    public void testNilMapIsNoOp() {
        assertZero(clear(GoKind.MAP, null), "clear(nil map) must return 0");
    }

    /** clear zeroes exactly [offset, offset+len) of a []float32 (R10-02). */
    public void testFloat32SliceCleared() {
        float[] arr = new float[] {1.0f, 2.0f, 3.0f, 4.0f};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 1, 2, 3)),
                "clear([]float32) must return 0");
        assertTrue(arr[0] == 1.0f, "clear must not touch elements before offset (got " + arr[0] + ")");
        assertTrue(arr[1] == 0.0f, "clear([]float32) must zero element 1 (got " + arr[1] + ")");
        assertTrue(arr[2] == 0.0f, "clear([]float32) must zero element 2 (got " + arr[2] + ")");
        assertTrue(arr[3] == 4.0f, "clear must not touch elements past len (got " + arr[3] + ")");
    }

    /** []float64 is zeroed over exactly the slice window (positive control). */
    public void testFloat64SliceCleared() {
        double[] arr = new double[] {1.0, 2.0, 3.0, 4.0};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 1, 2, 3)),
                "clear([]float64) must return 0");
        assertTrue(arr[0] == 1.0 && arr[1] == 0.0 && arr[2] == 0.0 && arr[3] == 4.0,
                "clear([]float64) must zero only elements 1..2 (got "
                        + arr[0] + "," + arr[1] + "," + arr[2] + "," + arr[3] + ")");
    }

    /** []int is zeroed over exactly the slice window (positive control). */
    public void testIntSliceCleared() {
        int[] arr = new int[] {9, 8, 7, 6};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 1, 2, 3)),
                "clear([]int) must return 0");
        assertTrue(arr[0] == 9 && arr[1] == 0 && arr[2] == 0 && arr[3] == 6,
                "clear([]int) must zero only elements 1..2 (got "
                        + arr[0] + "," + arr[1] + "," + arr[2] + "," + arr[3] + ")");
    }

    /** []byte is zeroed over exactly the slice window (positive control). */
    public void testByteSliceCleared() {
        byte[] arr = new byte[] {1, 2, 3};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 0, 3, 3)),
                "clear([]byte) must return 0");
        assertTrue(arr[0] == 0 && arr[1] == 0 && arr[2] == 0,
                "clear([]byte) must zero all elements");
    }

    /**
     * clear of a []complex64 restores the ZERO value, never null: a null
     * complex slot would surface as nil instead of 0+0i (R10-02 family —
     * Boxing.newArray pre-initialises complex arrays to ZERO).
     */
    public void testComplex64SliceCleared() {
        GoComplex32[] arr = new GoComplex32[] {
            new GoComplex32(1.0f, 2.0f), new GoComplex32(3.0f, 4.0f)};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 0, 2, 2)),
                "clear([]complex64) must return 0");
        assertTrue(arr[0] == GoComplex32.ZERO,
                "clear([]complex64) must restore ZERO, not null (got " + arr[0] + ")");
        assertTrue(arr[1] == GoComplex32.ZERO,
                "clear([]complex64) must restore ZERO, not null (got " + arr[1] + ")");
    }

    /** clear of a []complex128 restores the ZERO value, never null. */
    public void testComplex128SliceCleared() {
        GoComplex64[] arr = new GoComplex64[] {
            new GoComplex64(1.0, 2.0), new GoComplex64(3.0, 4.0)};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 0, 2, 2)),
                "clear([]complex128) must return 0");
        assertTrue(arr[0] == GoComplex64.ZERO,
                "clear([]complex128) must restore ZERO, not null (got " + arr[0] + ")");
        assertTrue(arr[1] == GoComplex64.ZERO,
                "clear([]complex128) must restore ZERO, not null (got " + arr[1] + ")");
    }

    /** []string is nulled (null is the zero for nil-able elements). */
    public void testObjectSliceCleared() {
        Object[] arr = new Object[] {"a", "b", "c"};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 0, 3, 3)),
                "clear([]string) must return 0");
        assertTrue(arr[0] == null && arr[1] == null && arr[2] == null,
                "clear([]string) must null all elements");
    }

    /** clear of a populated map empties it (positive control). */
    public void testMapCleared() {
        GoMap m = new GoMap();
        m.put("a", Integer.valueOf(1));
        m.put("b", Integer.valueOf(2));
        assertZero(clear(GoKind.MAP, m), "clear(map) must return 0");
        assertTrue(m.size() == 0, "clear(map) must empty the map (size " + m.size() + ")");
    }

    /** clear of an empty (len 0) slice leaves the backing array untouched. */
    public void testEmptySliceIsNoOp() {
        int[] arr = new int[] {5, 6, 7};
        assertZero(clear(GoKind.SLICE, new GoSlice(arr, 1, 0, 2)),
                "clear(empty slice) must return 0");
        assertTrue(arr[0] == 5 && arr[1] == 6 && arr[2] == 7,
                "clear(empty slice) must not touch the backing array");
    }

    /** executeObject clears too and yields null (mirrors p3builtin). */
    public void testExecuteObjectClearsAndReturnsNull() {
        float[] arr = new float[] {7.0f, 8.0f};
        Object result = new ClearNode(new ConstNode(GoKind.SLICE, new GoSlice(arr, 0, 2, 2)))
                .executeObject(null);
        assertTrue(result == null, "clear in value position must yield null");
        assertTrue(arr[0] == 0.0f && arr[1] == 0.0f,
                "executeObject must clear the slice (got " + arr[0] + "," + arr[1] + ")");
    }

    /** An unexpected operand kind fails fast instead of silently no-op. */
    public void testUnexpectedKindThrows() {
        try {
            clear(GoKind.INT64, Long.valueOf(3));
        } catch (FrontendException expected) {
            return;
        }
        throw new AssertionError(
                "clear of an INT64 operand must throw FrontendException, not silently return 0");
    }
}
