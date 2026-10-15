package graalgo.p3reflect;

import graalgo.values.GoKind;
import graalgo.values.GoSlice;

import java.lang.reflect.Method;

/**
 * Regression test for reviewer T2 (reflect-intSlice): {@code reflect.StructField.Index}
 * is a Go {@code []int}, and Go {@code int} = {@link GoKind#INT64} whose D5
 * backing store is {@code long[]}. The old {@code ReflectOpNode.intSlice}
 * built the slice on an {@code int[]} backing, so any kind-dispatched read
 * of {@code field.Index[i]} (e.g. {@link ReflectValues#arrayGet} with
 * {@link GoKind#INT64}) threw {@link ClassCastException}.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectOpNodeIntSliceTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoSlice intSlice(int[] vals) throws Exception {
        Method m = ReflectOpNode.class.getDeclaredMethod("intSlice", int[].class);
        m.setAccessible(true);
        return (GoSlice) m.invoke(null, (Object) vals);
    }

    /**
     * T2: the Index slice backing must be readable through the INT64
     * kind-dispatched path, with values identical to the input.
     * Fails with ClassCastException on the old int[]-backed implementation.
     */
    public void testStructFieldIndexSliceReadableAsInt64() throws Exception {
        int[] idx = {0, 2, 5};
        GoSlice sl = intSlice(idx);
        assertTrue(sl.len() == idx.length,
                "expected len " + idx.length + ", got " + sl.len());
        assertTrue(sl.array() instanceof long[],
                "backing for Go []int must be long[], got "
                        + sl.array().getClass().getSimpleName());
        for (int i = 0; i < idx.length; i++) {
            // Kind-dispatched read path — CCE on the old int[] backing.
            Object v = ReflectValues.arrayGet(sl.array(), GoKind.INT64, i);
            assertTrue(v instanceof Long,
                    "element " + i + " not boxed as Long: " + v);
            assertTrue(((Long) v).longValue() == idx[i],
                    "element " + i + ": expected " + idx[i] + ", got " + v);
        }
    }

    /** T2: an empty index path stays a valid empty slice. */
    public void testStructFieldIndexSliceEmpty() throws Exception {
        GoSlice sl = intSlice(new int[0]);
        assertTrue(sl.len() == 0, "expected len 0, got " + sl.len());
        assertTrue(sl.array() instanceof long[],
                "empty backing for Go []int must be long[], got "
                        + sl.array().getClass().getSimpleName());
    }
}
