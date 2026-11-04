package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoErrorValue;
import graalgo.runtime.Panic;
import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

/**
 * {@link MakeNode} range and hint semantics (REVIEW-2026-11-04-11 NEW-P0-4,
 * NEW-P1-1).
 *
 * <p>Slice form: len/cap arrive as int64 but {@code Intrinsics.makeslice}
 * takes ints. Before the fix, values passing the {@code len<0||cap<0||len>cap}
 * check were narrowed with a bare {@code (int)} cast: {@code make([]byte,
 * 1<<40)} silently returned an empty slice (the cast wraps to 0), and
 * {@code make([]byte, 1<<31)} threw a host
 * {@code NegativeArraySizeException} instead of a Go panic. gc panics
 * ("makeslice: len/cap out of range" once the request exceeds its maxAlloc;
 * verified against gc 1.27.1: {@code make([]byte,1<<62)} prints
 * {@code panic: runtime error: makeslice: len out of range}, and
 * {@code make([]byte,0,1<<62)} prints
 * {@code panic: runtime error: makeslice: cap out of range}). No Java array
 * holds more than {@code Integer.MAX_VALUE} elements, so the
 * unrepresentable range panics here with gc's messages — below gc's
 * maxAlloc threshold gc would allocate (or fatal-OOM) where the JVM cannot
 * represent the array at all, so a Go panic is the only faithful outcome.
 *
 * <p>Map form: before the fix, {@code NodeFactory} dropped the hint node
 * entirely, so a hint's side effects and panics never happened. gc evaluates
 * the hint exactly once. Negative hints do NOT panic: gc runtime/makemap
 * (go1.27.1) clamps {@code hint < 0} to 0, verified empirically (variable
 * hints of -1, -5, -100 and a function returning -5 all run without panic
 * under gc; only a constant negative hint is rejected, at compile time).
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
        // Also pins the rendering: the old raw-String panic degraded to
        // "panic: (?) 0xADDR"; the factory value renders gc's header.
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

    /** Hint node stand-in: records evaluation, returns a fixed value. */
    private static final class HintNode extends GoExprNode {
        private final long value;
        private final boolean[] evaluated;

        HintNode(long value, boolean[] evaluated) {
            super(GoKind.INT64);
            this.value = value;
            this.evaluated = evaluated;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            evaluated[0] = true;
            return value;
        }
    }

    public void testMapHintEvaluated() {
        boolean[] evaluated = new boolean[1];
        MakeNode n = new MakeNode(new HintNode(3, evaluated));
        Object o = n.executeObject(frame());
        if (!(o instanceof GoMap)) {
            throw new AssertionError("make(map, hint) returned " + o);
        }
        if (!evaluated[0]) {
            throw new AssertionError("make(map, hint) did not evaluate the hint");
        }
    }

    public void testMapHintNegativeIsNotAPanic() {
        // gc makemap clamps negative hints to 0; the map must come back fine.
        boolean[] evaluated = new boolean[1];
        MakeNode n = new MakeNode(new HintNode(-5, evaluated));
        Object o = n.executeObject(frame());
        if (!(o instanceof GoMap)) {
            throw new AssertionError("make(map, -5) returned " + o);
        }
        if (!evaluated[0]) {
            throw new AssertionError("make(map, -5) did not evaluate the hint");
        }
    }

    public void testMapHintPanicPropagates() {
        GoExprNode boom = new GoExprNode(GoKind.INT64) {
            @Override
            public long executeLong(VirtualFrame frame) {
                Panic.throwPanic("hint boom", null, null, null);
                throw new AssertionError("unreachable");
            }
        };
        MakeNode n = new MakeNode(boom);
        try {
            n.executeObject(frame());
        } catch (PanicException pe) {
            if (!"hint boom".equals(pe.goValue())) {
                throw new AssertionError("wrong propagated panic value: " + pe.goValue());
            }
            return;
        }
        throw new AssertionError("make(map, boom()) did not propagate the hint panic");
    }

    public void testMapConstantFormUnchanged() {
        // Legacy constant-hint form (frontend v2) still builds a map.
        Object o = new MakeNode(-1).executeObject(frame());
        if (!(o instanceof GoMap)) {
            throw new AssertionError("make(map) constant form returned " + o);
        }
    }
}
