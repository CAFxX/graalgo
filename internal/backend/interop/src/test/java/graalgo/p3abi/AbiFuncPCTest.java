package graalgo.p3abi;

import java.lang.reflect.Constructor;
import java.util.List;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.GoExprNode;
import graalgo.runtime.PanicException;
import graalgo.values.FuncIds;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * Unit tests for {@link AbiFuncPCNode} (R-03 remediation, 2026-10-31).
 *
 * <p>The node implements gc's {@code internal/abi} FuncPC intrinsics
 * on a backend with no machine PCs: it returns a stable opaque id
 * minted by {@link FuncIds} (0 for nil). The tests pin exactly that
 * contract: nil (Java null and nil interface) maps to 0, the same
 * function maps to the same non-zero id on repeated calls, distinct
 * functions map to distinct ids, an id roundtrips through
 * {@code FuncIds.funcFor}, and a non-func argument panics instead of
 * returning a plausible id.
 */
public final class AbiFuncPCTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    static final class ConstObj extends GoExprNode {
        private final Object v;

        ConstObj(Object v) {
            super(GoKind.INTERFACE);
            this.v = v;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return v;
        }
    }

    static final class CaptureRoot extends RootNode {
        CaptureRoot() {
            super(null);
        }

        @Override
        public Object execute(VirtualFrame frame) {
            return 0L;
        }
    }

    private static GoExprNode node(String kind, GoExprNode arg) throws Exception {
        Constructor<AbiFuncPCNode> c = AbiFuncPCNode.class.getDeclaredConstructor(
                String.class, GoExprNode.class);
        c.setAccessible(true);
        return c.newInstance(kind, arg);
    }

    private static GoFunc func() {
        return new GoFunc(new CaptureRoot().getCallTarget(), null, null);
    }

    public void testNilMapsToZero() throws Exception {
        VirtualFrame f = frame();
        assertEq(0, node("FuncPCABI0", new ConstObj(null)).executeLong(f),
                "Java-null argument must map to 0");
        assertEq(0, node("FuncPCABI0", new ConstObj(GoInterface.NIL)).executeLong(f),
                "nil interface argument must map to 0");
        assertEq(0, node("FuncPCABIInternal", new ConstObj(GoInterface.NIL)).executeLong(f),
                "nil interface must map to 0 for FuncPCABIInternal too");
    }

    public void testFuncIdIsStableNonZeroAndRoundtrips() throws Exception {
        VirtualFrame f = frame();
        GoFunc fn = func();
        GoExprNode n = node("FuncPCABI0", new ConstObj(fn));
        long id = n.executeLong(f);
        assertTrue(id != 0, "non-nil func id must be non-zero");
        assertEq(id, n.executeLong(f), "same func must map to the same id");
        assertEq(id, node("FuncPCABIInternal", new ConstObj(fn)).executeLong(f),
                "both ABI spellings must share the id (same function)");
        assertTrue(FuncIds.funcFor(id) == fn, "id must roundtrip to the same GoFunc");
        assertTrue(FuncIds.funcFor(0) == null, "id 0 must not resolve to a func");
    }

    public void testDistinctFuncsGetDistinctIds() throws Exception {
        VirtualFrame f = frame();
        long a = node("FuncPCABI0", new ConstObj(func())).executeLong(f);
        long b = node("FuncPCABI0", new ConstObj(func())).executeLong(f);
        assertTrue(a != b, "distinct functions must get distinct ids, got " + a + " twice");
    }

    public void testInterfaceWrappedFuncUnwraps() throws Exception {
        VirtualFrame f = frame();
        GoFunc fn = func();
        GoInterface iface = new GoInterface(
                new GoTypeDescriptor("main.F", GoKind.STRUCT, false), fn, null);
        long direct = node("FuncPCABI0", new ConstObj(fn)).executeLong(f);
        long wrapped = node("FuncPCABI0", new ConstObj(iface)).executeLong(f);
        assertEq(direct, wrapped, "interface-wrapped func must map to the same id");
    }

    public void testNonFuncPanics() throws Exception {
        VirtualFrame f = frame();
        GoExprNode n = node("FuncPCABI0",
                new ConstObj(GoString.fromUtf8("not a func")));
        try {
            n.executeLong(f);
            throw new AssertionError("non-func argument must panic");
        } catch (PanicException expected) {
            // Expected: the node must not mint an id for a non-func.
        }
    }
}
