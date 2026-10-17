package graalgo.frontend;

import java.nio.charset.StandardCharsets;
import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Pins {@code clear} slice semantics for every D5 backing carrier
 * (P0-4: {@code clear} on {@code []float32} was a silent no-op in the
 * frontend {@code ClearNode}; I-8 / review2-p19: {@code Object[]} slots
 * were nulled instead of receiving the Go zero value).
 *
 * <p>RED (pre-fix): {@link #testClearZeroesFloat32} fails (no
 * {@code float[]} branch); {@link #testClearZeroesComplex64} fails (no
 * complex-array branch); {@link #testClearStringSlice},
 * {@link #testClearComplexSlice}, {@link #testClearStructSlice} fail
 * (null written instead of the Go zero). GREEN: every carrier zeroes to
 * the Go zero value; only {@code len} elements are touched; nil slices
 * and empty ranges are no-ops.
 *
 * <p>Style follows the module's red-green runner (public no-arg
 * {@code test*} methods, {@link AssertionError} on failure, registered
 * in {@code graalgo.test.Runner}).
 */
public final class ClearSliceTest {

    /** A stub operand node returning a fixed value. */
    private static final class Const extends GoExprNode {
        private final Object value;

        Const(GoKind kind, Object value) {
            super(kind);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    // --- P0-4 regression: float32 (and char) through the frontend node ---

    public void testClearZeroesFloat32() {
        float[] arr = new float[] { 1.5f, -2.25f, 3.75f };
        GoSlice s = new GoSlice(arr, 0, 3, 3);
        long r = new ClearNode(new Const(GoKind.SLICE, s), GoKind.FLOAT32).executeLong(null);
        if (r != 0L) {
            throw new AssertionError("clear must return 0, got " + r);
        }
        for (float f : arr) {
            if (f != 0.0f) {
                throw new AssertionError("float32 slot not zeroed: " + f);
            }
        }
    }

    public void testClearZeroesComplex64() {
        GoComplex32[] arr = new GoComplex32[] {
                new GoComplex32(1f, 2f), new GoComplex32(3f, 4f) };
        GoSlice s = new GoSlice(arr, 0, 2, 2);
        new ClearNode(new Const(GoKind.SLICE, s), GoKind.COMPLEX64).executeLong(null);
        for (GoComplex32 c : arr) {
            if (c == null || c.re() != 0f || c.im() != 0f) {
                throw new AssertionError("complex64 slot not zeroed: " + c);
            }
        }
    }

    public void testClearZeroesComplex128() {
        GoComplex64[] arr = new GoComplex64[] { new GoComplex64(1, 2) };
        GoSlice s = new GoSlice(arr, 0, 1, 1);
        new ClearNode(new Const(GoKind.SLICE, s), GoKind.COMPLEX128).executeLong(null);
        GoComplex64 c = arr[0];
        if (c == null || c.re() != 0 || c.im() != 0) {
            throw new AssertionError("complex128 slot not zeroed: " + c);
        }
    }

    // --- Full carrier matrix through the canonical Boxing.clearSlice ---

    public void testClearPrimitiveCarriers() {
        assertPrimitiveCleared(GoKind.INT8, new byte[] { 1, 2, 3 });
        assertPrimitiveCleared(GoKind.UINT8, new byte[] { (byte) 200, 7 });
        assertPrimitiveCleared(GoKind.INT16, new short[] { 300, -400 });
        assertPrimitiveCleared(GoKind.UINT16, new short[] { (short) 60000 });
        assertPrimitiveCleared(GoKind.INT32, new int[] { 1 << 30, -5 });
        assertPrimitiveCleared(GoKind.UINT32, new int[] { -1 });
        assertPrimitiveCleared(GoKind.INT64, new long[] { Long.MAX_VALUE, Long.MIN_VALUE });
        assertPrimitiveCleared(GoKind.UINT64, new long[] { -1L });
        assertPrimitiveCleared(GoKind.FLOAT32, new float[] { 1.5f, Float.NaN });
        assertPrimitiveCleared(GoKind.FLOAT64, new double[] { 2.5, Double.NaN });
        assertPrimitiveCleared(GoKind.BOOL, new boolean[] { true, true });
        // char[] is defensive (D5 never emits it) but the branch must work.
        char[] chars = new char[] { 'a', 'b' };
        Boxing.clearSlice(new GoSlice(chars, 0, 2, 2), GoKind.UINT16, null);
        for (char c : chars) {
            if (c != '\0') {
                throw new AssertionError("char slot not zeroed: " + (int) c);
            }
        }
    }

    private static void assertPrimitiveCleared(GoKind kind, Object array) {
        int n = java.lang.reflect.Array.getLength(array);
        Boxing.clearSlice(new GoSlice(array, 0, n, n), kind, null);
        for (int i = 0; i < n; i++) {
            Object v = java.lang.reflect.Array.get(array, i);
            if (!isZeroBox(v)) {
                throw new AssertionError(kind + " slot " + i + " not zeroed: " + v);
            }
        }
    }

    private static boolean isZeroBox(Object v) {
        return (v instanceof Byte b && b == 0)
                || (v instanceof Short sh && sh == 0)
                || (v instanceof Integer in && in == 0)
                || (v instanceof Long l && l == 0L)
                || (v instanceof Float f && f == 0.0f)
                || (v instanceof Double d && d == 0.0)
                || (v instanceof Boolean bo && !bo)
                || (v instanceof Character c && c == '\0');
    }

    public void testClearStringSlice() {
        Object[] arr = new Object[] {
                new GoString("hi".getBytes(StandardCharsets.UTF_8)),
                new GoString("yo".getBytes(StandardCharsets.UTF_8)) };
        Boxing.clearSlice(new GoSlice(arr, 0, 2, 2), GoKind.STRING, null);
        for (Object o : arr) {
            if (o != GoString.EMPTY) {
                throw new AssertionError("string slot not EMPTY: " + o);
            }
        }
    }

    public void testClearInterfaceSliceKeepsNull() {
        // []any's zero IS null (GoEquality: null elements hash as the zero
        // value) — the old nulling behavior is correct here.
        Object[] arr = new Object[] {
                new GoString("hi".getBytes(StandardCharsets.UTF_8)), Integer.valueOf(3) };
        Boxing.clearSlice(new GoSlice(arr, 0, 2, 2), GoKind.INTERFACE, null);
        for (Object o : arr) {
            if (o != null) {
                throw new AssertionError("interface slot not nulled: " + o);
            }
        }
    }

    public void testClearComplexSliceInObjectArray() {
        // Defensive: complex elements in a legacy Object[] carrier.
        Object[] arr = new Object[] { new GoComplex32(1f, 2f), new GoComplex32(3f, 4f) };
        Boxing.clearSlice(new GoSlice(arr, 0, 2, 2), GoKind.COMPLEX64, null);
        for (Object o : arr) {
            if (o != GoComplex32.ZERO) {
                throw new AssertionError("complex64 slot not ZERO: " + o);
            }
        }
        Object[] arr128 = new Object[] { new GoComplex64(1, 2) };
        Boxing.clearSlice(new GoSlice(arr128, 0, 1, 1), GoKind.COMPLEX128, null);
        if (arr128[0] != GoComplex64.ZERO) {
            throw new AssertionError("complex128 slot not ZERO: " + arr128[0]);
        }
    }

    private static GoStructDescriptor structDesc() {
        return new GoStructDescriptor("T", List.of(
                new GoStructDescriptor.Field("n", GoKind.INT64, true, 0, -1, null, null, null),
                new GoStructDescriptor.Field("s", GoKind.STRING, false, 0, -1, null, null, null)));
    }

    public void testClearStructSlice() {
        GoStructDescriptor desc = structDesc();
        Object arr = Boxing.newArray(GoKind.STRUCT, desc, 2);
        Object[] oa = (Object[]) arr;
        // Dirty the slots first: the clear must restore true zeros.
        GoStruct g0 = (GoStruct) oa[0];
        g0.setLong(0, 42L);
        g0.setObject(1, new GoString("x".getBytes(StandardCharsets.UTF_8)));
        Boxing.clearSlice(new GoSlice(arr, 0, 2, 2), GoKind.STRUCT, desc);
        GoStruct c0 = (GoStruct) oa[0];
        GoStruct c1 = (GoStruct) oa[1];
        if (c0.getLong(0) != 0L || c1.getLong(0) != 0L) {
            throw new AssertionError("struct int field not zeroed");
        }
        if (c0.getObject(1) != GoString.EMPTY || c1.getObject(1) != GoString.EMPTY) {
            throw new AssertionError("struct string field not EMPTY: " + c0.getObject(1));
        }
        if (c0 == c1) {
            throw new AssertionError("struct zero slots must not alias (GoStruct is mutable)");
        }
    }

    public void testClearStructSliceRecoversDescriptor() {
        // No descriptor passed (v2 frontend path): recovered from a live
        // element, which carries its own descriptor.
        GoStructDescriptor desc = structDesc();
        Object arr = Boxing.newArray(GoKind.STRUCT, desc, 2);
        Object[] oa = (Object[]) arr;
        ((GoStruct) oa[0]).setLong(0, 7L);
        Boxing.clearSlice(new GoSlice(arr, 0, 2, 2), GoKind.STRUCT, null);
        if (((GoStruct) oa[0]).getLong(0) != 0L || ((GoStruct) oa[1]).getLong(0) != 0L) {
            throw new AssertionError("struct slots not zeroed via recovered descriptor");
        }
    }

    public void testClearOnlyLenElements() {
        // Backing array beyond len(s) is untouched (gc parity).
        int[] arr = new int[] { 9, 8, 7, 6 };
        GoSlice s = new GoSlice(arr, 1, 2, 3); // clears indices 1..2 only
        Boxing.clearSlice(s, GoKind.INT32, null);
        if (arr[0] != 9 || arr[3] != 6) {
            throw new AssertionError("clear touched outside len: "
                    + arr[0] + "," + arr[3]);
        }
        if (arr[1] != 0 || arr[2] != 0) {
            throw new AssertionError("clear missed in-len slots");
        }
    }

    public void testClearEmptyIsNoop() {
        int[] arr = new int[] { 5 };
        Boxing.clearSlice(new GoSlice(arr, 0, 0, 1), GoKind.INT32, null);
        if (arr[0] != 5) {
            throw new AssertionError("empty clear must not touch the array");
        }
    }

    public void testClearNilSliceIsNoop() {
        // The frontend node treats a nil slice as a no-op (gc parity).
        long r = new ClearNode(new Const(GoKind.SLICE, null)).executeLong(null);
        if (r != 0L) {
            throw new AssertionError("clear(nil) must return 0");
        }
    }

    public void testClearMapDeletesEntries() {
        GoMap m = new GoMap();
        m.put(key("a"), Integer.valueOf(1));
        m.put(key("b"), Integer.valueOf(2));
        new ClearNode(new Const(GoKind.MAP, m)).executeLong(null);
        if (m.size() != 0) {
            throw new AssertionError("clear(map) left " + m.size() + " entries");
        }
        // Nil map is a no-op.
        new ClearNode(new Const(GoKind.MAP, null)).executeLong(null);
    }

    private static Object key(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }
}
