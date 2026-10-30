package graalgo.frontend;

import java.util.ArrayList;
import java.util.List;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Unit tests for the GoFuncRootNode value-copy choke point (N-P1-3):
 * by-value STRUCT/ARRAY parameters are deep-copied on every invocation,
 * for every call path. The body mutates its parameter; the caller must
 * never observe the mutation.
 *
 * <p>Why the root is the choke point: a direct call already copies at
 * the call site, but interface dispatch passes the receiver raw and a
 * method value binds its receiver raw — and a bind-time copy would
 * still leak across repeated invocations. The receiver of an interface
 * dispatch and the bound receiver of a method value both travel as
 * params[0] into this root, so copying here covers all three paths
 * with one rule, per invocation.
 */
public final class GoFuncRootNodeValueCopyTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /** Body that records the struct parameter's x, then sets x to 99. */
    static final class MutateStructBody extends GoStmtNode {
        private final int paramSlot;
        final List<Long> seen = new ArrayList<>();

        MutateStructBody(int paramSlot) {
            this.paramSlot = paramSlot;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            GoStruct s = (GoStruct) frame.getObject(paramSlot);
            seen.add(s.getLong(0));
            s.setLong(0, 99);
        }
    }

    /** Body that records the array parameter's [0], then sets [0] to 99. */
    static final class MutateArrayBody extends GoStmtNode {
        private final int paramSlot;
        final List<Integer> seen = new ArrayList<>();

        MutateArrayBody(int paramSlot) {
            this.paramSlot = paramSlot;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            int[] a = (int[]) frame.getObject(paramSlot);
            seen.add(a[0]);
            a[0] = 99;
        }
    }

    private static GoFuncRootNode rootFor(GoStmtNode body, int closureSlot, int paramSlot,
            GoKind paramKind, String name) {
        FrameDescriptor.Builder b = FrameDescriptor.newBuilder();
        // Slots must exist in the descriptor at the indices the root uses.
        // The builder assigns indices in creation order: closure first,
        // then the parameter slot.
        int closure = b.addSlot(FrameSlotKind.Object, null, null);
        int param = b.addSlot(FrameSlotKind.Object, null, null);
        assertTrue(closure == closureSlot, "closure slot index");
        assertTrue(param == paramSlot, "param slot index");
        return new GoFuncRootNode(b.build(), body, closureSlot, new int[]{paramSlot},
                new GoKind[]{paramKind}, -1, GoKind.INTERFACE, name);
    }

    /**
     * N-P1-3, method-value path: the bound receiver is copied per
     * invocation. Invoking the same method value twice must observe
     * the ORIGINAL value both times — red: the second invocation saw
     * 99, the first invocation's mutation of the shared bound object.
     */
    public void testBoundReceiverCopiedPerInvocation() {
        GoStructDescriptor desc = GoStructDescriptor.layout("T",
                List.of("x"), List.of(GoKind.INT64));
        GoStruct orig = new GoStruct(desc, new long[]{7}, new Object[0]);
        MutateStructBody body = new MutateStructBody(1);
        GoFuncRootNode root = rootFor(body, 0, 1, GoKind.STRUCT, "T.M");
        GoFunc fn = new GoFunc(root.getCallTarget(), null, null, new Object[]{orig});
        GoCalls.invoke(fn, new Object[0]);
        GoCalls.invoke(fn, new Object[0]);
        assertEquals(2, body.seen.size(), "two invocations observed");
        assertEquals(7L, body.seen.get(0), "first invocation sees the original");
        assertEquals(7L, body.seen.get(1), "second invocation sees the original, not the mutation");
        assertEquals(7L, orig.getLong(0), "caller's struct never mutated");
    }

    /**
     * N-P1-3, direct/iface-dispatch path: a STRUCT parameter is copied
     * per invocation, so a callee mutating its parameter cannot affect
     * the caller's value (the interface-dispatch receiver travels as
     * params[0] through this same root).
     */
    public void testStructParamCopiedPerInvocation() {
        GoStructDescriptor desc = GoStructDescriptor.layout("T",
                List.of("x"), List.of(GoKind.INT64));
        GoStruct orig = new GoStruct(desc, new long[]{3}, new Object[0]);
        MutateStructBody body = new MutateStructBody(1);
        GoFuncRootNode root = rootFor(body, 0, 1, GoKind.STRUCT, "f");
        GoFunc fn = new GoFunc(root.getCallTarget(), null, null);
        GoCalls.invoke(fn, new Object[]{orig});
        assertEquals(1, body.seen.size(), "one invocation observed");
        assertEquals(3L, body.seen.get(0), "callee sees the argument value");
        assertEquals(3L, orig.getLong(0), "caller's struct never mutated");
    }

    /**
     * N-P1-3, array path: an ARRAY parameter is deep-copied per
     * invocation (struct elements included via deepCopyArray).
     */
    public void testArrayParamCopiedPerInvocation() {
        int[] orig = new int[]{4, 5};
        MutateArrayBody body = new MutateArrayBody(1);
        GoFuncRootNode root = rootFor(body, 0, 1, GoKind.ARRAY, "g");
        GoFunc fn = new GoFunc(root.getCallTarget(), null, null);
        GoCalls.invoke(fn, new Object[]{orig});
        assertEquals(1, body.seen.size(), "one invocation observed");
        assertEquals(4, body.seen.get(0), "callee sees the argument value");
        assertEquals(4, orig[0], "caller's array never mutated");
    }
}
