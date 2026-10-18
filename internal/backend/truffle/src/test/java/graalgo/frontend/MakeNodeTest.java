package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoErrorValue;
import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * RV18-P1-13: {@link MakeNode} must not silently narrow huge len/cap to
 * {@code int}. Before the fix, {@code make([]byte, 1<<40)} passed the
 * {@code len<0||cap<0||len>cap} check and the {@code (int)} cast wrapped to
 * 0, silently returning an empty slice. gc panics ("makeslice: len/cap out
 * of range" once the request exceeds its maxAlloc; verified against gc
 * 1.27.1: {@code make([]byte, 1<<62)} prints
 * {@code panic: runtime error: makeslice: len out of range}, and
 * {@code make([]byte, 0, 1<<62)} prints
 * {@code panic: runtime error: makeslice: cap out of range}).
 *
 * <p>Red-green: {@code testLenHugePanics}, {@code testCapHugePanics}, and
 * the boundary tests fail before the fix (no panic is thrown — the node
 * returns a wrapped slice) and pass after. {@code testSmallMakesWork} and
 * {@code testNegativeLenStillPanics} are controls that pass before and
 * after.
 */
public final class MakeNodeTest {

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0], new FrameDescriptor());
    }

    private static GoExprNode intConst(long v) {
        return ConstNode.ofInt(GoKind.INT64, v);
    }

    /** Runs make([]byte, len[, cap]) and returns the thrown panic's message. */
    private static String panicMessage(long len, Long cap) {
        MakeNode n = new MakeNode(GoKind.UINT8, intConst(len),
                cap == null ? null : intConst(cap), null);
        try {
            n.executeObject(frame());
        } catch (PanicException pe) {
            Object v = pe.goValue();
            if (v instanceof GoErrorValue e) {
                return e.errorString();
            }
            throw new AssertionError("panic value is not a GoErrorValue: " + v);
        }
        throw new AssertionError(
                "make([]byte, " + len + (cap == null ? "" : ", " + cap) + ") did not panic");
    }

    private static void assertPanicMessage(long len, Long cap, String want) {
        String got = panicMessage(len, cap);
        if (!want.equals(got)) {
            throw new AssertionError("make([]byte, " + len
                    + (cap == null ? "" : ", " + cap) + "): want panic <" + want
                    + "> got <" + got + ">");
        }
    }

    public void testLenHugePanics() {
        // 1<<40 wraps to 0 under the old (int) cast.
        assertPanicMessage(1L << 40, null, "runtime error: makeslice: len out of range");
    }

    public void testCapHugePanics() {
        assertPanicMessage(3L, 1L << 40, "runtime error: makeslice: cap out of range");
    }

    public void testLenBoundaryPanics() {
        // Integer.MAX_VALUE itself is representable; MAX_VALUE+1 is not.
        assertPanicMessage((long) Integer.MAX_VALUE + 1, null,
                "runtime error: makeslice: len out of range");
    }

    public void testCapBoundaryPanics() {
        assertPanicMessage(0L, (long) Integer.MAX_VALUE + 1,
                "runtime error: makeslice: cap out of range");
    }

    public void testLenHugeBeatsCapHuge() {
        // gc checks len before cap; both out of range -> len message.
        assertPanicMessage(1L << 40, 1L << 62,
                "runtime error: makeslice: len out of range");
    }

    public void testNegativeLenStillPanics() {
        assertPanicMessage(-1L, null, "runtime error: makeslice: len out of range");
    }

    public void testSmallMakesWork() {
        assertSlice(3L, null, 3, 3);
        assertSlice(3L, 5L, 3, 5);
        assertSlice(0L, null, 0, 0);
    }

    private static void assertSlice(long len, Long cap, int wantLen, int wantCap) {
        MakeNode n = new MakeNode(GoKind.UINT8, intConst(len),
                cap == null ? null : intConst(cap), null);
        Object o = n.executeObject(frame());
        if (!(o instanceof GoSlice s)) {
            throw new AssertionError("make([]byte, " + len + ") returned " + o);
        }
        if (s.len() != wantLen || s.cap() != wantCap) {
            throw new AssertionError("make([]byte, " + len
                    + (cap == null ? "" : ", " + cap) + "): want len=" + wantLen
                    + " cap=" + wantCap + " got len=" + s.len() + " cap=" + s.cap());
        }
    }
}
