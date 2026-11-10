package graalgo.frontend;

import java.util.List;
import java.util.Map;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;

/**
 * R22-F-4: a method value must snapshot a value receiver at formation
 * time, and every invocation must receive a fresh copy — callee mutations
 * (and mutations of the original after formation) must never alias the
 * caller, and repeated calls must be independent.
 *
 * <p>RED (pre-fix): {@link MethodValueNode} binds the raw receiver and
 * {@link GoCalls#callArgs} prepends it verbatim, so all three alias.
 * Registered in {@code graalgo.test.Runner}.
 */
public final class MethodValueCopyTest {

    /** Descriptor for struct{F int64}: one long field at index 0. */
    private static GoStructDescriptor structDesc() {
        return new GoStructDescriptor("S", List.of(
                new GoStructDescriptor.Field("F", GoKind.INT64, true, 0, -1, null, null, null)));
    }

    private static GoStruct structWith(long f) {
        return new GoStruct(structDesc(), new long[]{f}, new Object[0]);
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** A GoExprNode evaluating to a constant object. */
    static final class ConstNode extends GoExprNode {
        private final Object objVal;

        ConstNode(GoKind kind, Object objVal) {
            super(kind);
            this.objVal = objVal;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return objVal;
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    /**
     * A method root whose receiver parameter has the given kind. The body
     * is empty (never executed here — only the call target / param kinds
     * are consulted); slot indices are irrelevant without execution.
     */
    private static GoFuncRootNode methodRoot(GoKind receiverKind) {
        FrameDescriptor desc = FrameDescriptor.newBuilder().build();
        return new GoFuncRootNode(desc, new BlockNode(new GoStmtNode[0]),
                0, new int[]{1}, new GoKind[]{receiverKind},
                2, GoKind.INT64, "M");
    }

    private static GoFunc goFunc(GoFuncRootNode root) {
        return new GoFunc(root.getCallTarget(), null, null);
    }

    /**
     * Formation snapshot: binding a value receiver copies it — mutating
     * the original after {@code f := s.M} must not change what {@code f}
     * sees (the bound copy keeps the formation-time value).
     */
    public void testMethodValueBindsCopyOfValueReceiver() {
        GoStruct s = structWith(1);
        GoFunc f = goFunc(methodRoot(GoKind.STRUCT));
        MethodValueNode n = new MethodValueNode(new ConstNode(GoKind.STRUCT, s),
                Map.of(7, f), 7);
        GoFunc bound = (GoFunc) n.executeObject(frame());
        Object recv = bound.boundArgs()[0];
        assertTrue(recv != s,
                "MethodValueNode bound the raw receiver (R22-F-4)");
        assertTrue(recv instanceof GoStruct gs && gs.getLong(0) == 1,
                "bound receiver has wrong value");
        s.setLong(0, 5);
        assertTrue(((GoStruct) recv).getLong(0) == 1,
                "mutating the original changed the bound receiver (R22-F-4): F="
                        + ((GoStruct) recv).getLong(0));
    }

    /**
     * Per-invocation freshness: each {@code GoCalls.callArgs} on a method
     * value yields a distinct receiver copy, so mutations from one call
     * never leak into the next (or back to the bound value).
     */
    public void testMethodValueCallGetsFreshReceiverPerInvocation() {
        GoStruct s = structWith(1);
        GoFunc f = new GoFunc(methodRoot(GoKind.STRUCT).getCallTarget(), null, null,
                new Object[]{s});
        Object[] call1 = GoCalls.callArgs(f, new Object[0]);
        Object[] call2 = GoCalls.callArgs(f, new Object[0]);
        assertTrue(call1[1] != s && call2[1] != s,
                "GoCalls.callArgs passed the bound receiver verbatim (R22-F-4)");
        assertTrue(call1[1] != call2[1],
                "two invocations shared one receiver copy (R22-F-4)");
        ((GoStruct) call1[1]).setLong(0, 99);
        assertTrue(s.getLong(0) == 1,
                "mutating the call receiver mutated the bound value (R22-F-4)");
        assertTrue(((GoStruct) call2[1]).getLong(0) == 1,
                "mutating one call's receiver leaked into the next call (R22-F-4)");
    }

    /**
     * Negative control: a pointer receiver stays shared — the bound value
     * and every call see the same object, so callee mutations are visible
     * to the caller exactly like gc.
     */
    public void testPointerReceiverStaysShared() {
        GoStruct s = structWith(1);
        GoFunc f = goFunc(methodRoot(GoKind.POINTER));
        MethodValueNode n = new MethodValueNode(new ConstNode(GoKind.STRUCT, s),
                Map.of(7, f), 7);
        GoFunc bound = (GoFunc) n.executeObject(frame());
        assertTrue(bound.boundArgs()[0] == s,
                "pointer receiver was copied — mutations would stop being visible");
        Object[] call = GoCalls.callArgs(bound, new Object[0]);
        assertTrue(call[1] == s,
                "pointer receiver was copied at the call — mutations would stop being visible");
    }

    /**
     * Interface-typed receiver parameter (desugared helper): the bound
     * interface value itself passes through — the header is shared, and
     * no data copy is made at this layer.
     */
    public void testInterfaceReceiverParamPassesThrough() {
        GoStruct s = structWith(1);
        GoInterface gi = new GoInterface(
                new GoTypeDescriptor("main.S", GoKind.STRUCT, false), s, null);
        GoFunc f = goFunc(methodRoot(GoKind.INTERFACE));
        MethodValueNode n = new MethodValueNode(new ConstNode(GoKind.INTERFACE, gi),
                Map.of(7, f), 7);
        GoFunc bound = (GoFunc) n.executeObject(frame());
        assertTrue(bound.boundArgs()[0] == gi,
                "interface receiver param must pass the interface value through");
    }

    /**
     * A method value formed on an interface value with a value receiver:
     * the struct data inside the interface is copied at formation, so the
     * bound receiver does not alias the interface's data.
     */
    public void testMethodValueOnInterfaceCopiesStructData() {
        GoStruct s = structWith(1);
        GoInterface gi = new GoInterface(
                new GoTypeDescriptor("main.S", GoKind.STRUCT, false), s, null);
        GoFunc f = goFunc(methodRoot(GoKind.STRUCT));
        MethodValueNode n = new MethodValueNode(new ConstNode(GoKind.INTERFACE, gi),
                Map.of(7, f), 7);
        GoFunc bound = (GoFunc) n.executeObject(frame());
        Object recv = bound.boundArgs()[0];
        assertTrue(recv instanceof GoInterface rgi && rgi.data() != s,
                "method value on an interface shared the struct data (R22-F-4)");
        ((GoStruct) ((GoInterface) recv).data()).setLong(0, 99);
        assertTrue(s.getLong(0) == 1,
                "mutating the bound interface data mutated the original (R22-F-4)");
    }

    /**
     * Plain function values have no bound receiver: callArgs passes
     * explicit arguments through untouched (copying them is the call
     * site's job — FuncValueCallNode — not GoCalls').
     */
    public void testPlainFuncCallArgsPassThrough() {
        GoStruct s = structWith(1);
        GoFunc f = goFunc(methodRoot(GoKind.STRUCT));
        Object[] call = GoCalls.callArgs(f, new Object[]{s});
        assertTrue(call[1] == s,
                "GoCalls.callArgs must not copy explicit arguments");
    }
}
