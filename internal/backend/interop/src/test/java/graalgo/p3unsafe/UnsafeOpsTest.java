package graalgo.p3unsafe;

import java.util.List;

import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Unit tests for {@link UnsafeOps} (review3-traps P1-I2).
 *
 * <p>Red-green discipline: the struct-referent test pins the current
 * documented failure mode (loud Go panic, B-8) — it fails if the panic
 * goes silent or the message changes, and it flips to a view test when
 * the B-8 byte-view carrier lands. The byte-array test is the positive
 * control: the same call shape succeeds on a supported referent.
 */
public final class UnsafeOpsTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        boolean eq = expected == null ? actual == null : expected.equals(actual);
        if (!eq) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static void setUp() {
        UnsafeTable.install();
    }

    private static GoStruct structReferent() {
        GoStructDescriptor desc = new GoStructDescriptor("main.S", List.of(
                new GoStructDescriptor.Field("x", GoKind.INT64, true,
                        0, -1, null, null, null)));
        return new GoStruct(desc, new long[]{7L}, new Object[0]);
    }

    /**
     * B-8: unsafe.Slice over a GoStruct referent fails loud with a Go
     * panic naming the unsupported shape — never silently, never a JVM
     * exception. (Flips to a byte-view test when the B-8 carrier lands.)
     */
    public void testSliceOnStructReferentPanics() {
        setUp();
        try {
            UnsafeOps.slice(structReferent(), 8, GoKind.UINT8, null);
        } catch (PanicException pe) {
            assertEquals("unsafe.Slice: unsupported referent shape GoStruct",
                    pe.goValue(), "documented B-8 failure message");
            return;
        }
        throw new AssertionError("expected PanicException for struct referent");
    }

    /**
     * B-8: unsafe.String over a GoStruct referent fails loud the same way.
     */
    public void testStringOnStructReferentPanics() {
        setUp();
        try {
            UnsafeOps.stringOf(structReferent(), 8, null);
        } catch (PanicException pe) {
            assertEquals("unsafe.String: unsupported referent shape GoStruct",
                    pe.goValue(), "documented B-8 failure message");
            return;
        }
        throw new AssertionError("expected PanicException for struct referent");
    }

    /**
     * Positive control: the same unsafe.Slice call shape succeeds on a
     * supported (byte-array) referent.
     */
    public void testSliceOnByteArrayReferent() {
        setUp();
        byte[] arr = new byte[]{1, 2, 3, 4};
        GoSlice sl = UnsafeOps.slice(arr, 4, GoKind.UINT8, null);
        assertTrue(sl != null, "non-nil slice");
        assertEquals(4, sl.len(), "len");
        assertEquals(4, sl.cap(), "cap");
    }

    /**
     * Positive control: nil pointer with len 0 is the nil slice (null),
     * not a panic.
     */
    public void testSliceOnNilReferentLenZero() {
        setUp();
        assertEquals(null, UnsafeOps.slice(null, 0, GoKind.UINT8, null),
                "nil slice");
    }
}
