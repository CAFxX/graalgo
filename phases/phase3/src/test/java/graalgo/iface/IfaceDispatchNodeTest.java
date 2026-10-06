package graalgo.iface;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.runtime.GoErrorValue;
import graalgo.runtime.GoErrors;
import graalgo.runtime.PanicException;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;
import graalgo.values.InterfaceDispatchProbes;

/**
 * Unit tests for {@link IfaceDispatchNode} (P3-M3, WS D, ADR-0003).
 *
 * <p>Red-green discipline: each test fails against a broken dispatch
 * (stubbed table, identity-compare replaced by equals, missing panic,
 * missing probe record) and passes against the real one.
 * verifies: GG-0014
 */
public final class IfaceDispatchNodeTest {

    // ---------- harness ----------

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

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** A GoExprNode evaluating to a constant. */
    static final class ConstNode extends GoExprNode {
        private final Object objVal;
        private final long longVal;

        ConstNode(GoKind kind, Object objVal, long longVal) {
            super(kind);
            this.objVal = objVal;
            this.longVal = longVal;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return objVal;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return longVal;
        }
    }

    /** A RootNode capturing its call arguments and returning a constant. */
    static final class CaptureRoot extends RootNode {
        final List<Object[]> seen = new ArrayList<>();
        private final Object ret;

        CaptureRoot(Object ret) {
            super(null);
            this.ret = ret;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            seen.add(frame.getArguments().clone());
            return ret;
        }
    }

    private static graalgo.values.GoFunc goFunc(CaptureRoot root) {
        RootCallTarget target = root.getCallTarget();
        return new graalgo.values.GoFunc(target, null, null);
    }

    /** Direct (non-promoted) method target. */
    private static IfaceMethodTarget target(CaptureRoot root) {
        return new IfaceMethodTarget(goFunc(root), new int[0]);
    }

    /** Promoted method target: receiver adapted via the field path. */
    private static IfaceMethodTarget promotedTarget(CaptureRoot root, int... recvPath) {
        return new IfaceMethodTarget(goFunc(root), recvPath);
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    private static GoInterface iface(String descName, Object data) {
        return new GoInterface(new GoTypeDescriptor(descName, GoKind.STRUCT, false), data, null);
    }

    private IfaceDispatchNode node(GoExprNode recv, GoExprNode[] args, List<String> methodNames,
            int ifaceId) {
        return new IfaceDispatchNode(recv, args, "M", methodNames, ifaceId,
                GoKind.INT64, -1);
    }

    private void setUp(Map<String, Map<String, IfaceMethodTarget>> table) {
        IfaceMethodTable.clear();
        IfaceDispatchNode.resetMegamorphicTransitions();
        IfaceMethodTable.install(table);
    }

    private void tearDown() {
        IfaceMethodTable.clear();
        IfaceDispatchNode.resetMegamorphicTransitions();
        InterfaceDispatchProbes.setEnabled(false);
        InterfaceDispatchProbes.reset();
    }

    // ---------- tests ----------

    /**
     * Same itab twice → one itab build (PIC hit), correct receiver+args.
     * verifies: GG-0014
     */
    public void testPicHit() {
        CaptureRoot root = new CaptureRoot(42L);
        setUp(Map.of("main.T", Map.of("M", target(root))));
        try {
            GoExprNode recv = new ConstNode(GoKind.INTERFACE, iface("main.T", "DATA"), 0);
            GoExprNode[] args = {new ConstNode(GoKind.INT64, null, 7L)};
            IfaceDispatchNode n = node(recv, args, List.of("M"), 3);
            VirtualFrame f = frame();
            assertEquals(42L, n.executeLong(f), "first dispatch result");
            assertEquals(42L, n.executeLong(f), "second dispatch result");
            assertEquals(1, IfaceMethodTable.itabCacheSize(), "one itab built for two dispatches");
            assertEquals(0, IfaceDispatchNode.megamorphicTransitions(), "no megamorphic transition");
            // Receiver's data is params[0], then the call args (GoCalls
            // prepends the GoFunc itself at callArgs[0]).
            assertEquals(2, root.seen.size(), "method invoked twice");
            assertEquals("DATA", root.seen.get(0)[1], "params[0] is the receiver data");
            assertEquals(7L, root.seen.get(0)[2], "params[1] is the call arg");
        } finally {
            tearDown();
        }
    }

    /**
     * Four distinct itabs through one site → megamorphic fallback, still
     * correct results, exactly one transition counted, no further
     * transitions on megamorphic hits.
     * verifies: GG-0014
     */
    public void testPicOverflowMegamorphic() {
        CaptureRoot[] roots = new CaptureRoot[4];
        java.util.Map<String, Map<String, IfaceMethodTarget>> table = new java.util.HashMap<>();
        for (int i = 0; i < 4; i++) {
            roots[i] = new CaptureRoot((long) (i + 1));
            table.put("main.T" + i, Map.of("M", target(roots[i])));
        }
        setUp(table);
        try {
            VirtualFrame f = frame();
            MutableRecv mutable = new MutableRecv(iface("main.T0", "d0"));
            IfaceDispatchNode shared = node(mutable, new GoExprNode[0], List.of("M"), 3);
            // Three distinct itabs fill the PIC; the fourth overflows it.
            for (int i = 0; i < 4; i++) {
                mutable.value = iface("main.T" + i, "d" + i);
                assertEquals((long) (i + 1), shared.executeLong(f),
                        "shared-site dispatch " + i);
            }
            assertEquals(1, IfaceDispatchNode.megamorphicTransitions(),
                    "exactly one megamorphic transition");
            assertTrue(shared.toString().contains("megamorphic=true"),
                    "toString reports megamorphic");
            assertEquals(4, IfaceMethodTable.itabCacheSize(), "four itabs built");
            // Megamorphic hits stay correct and count no further transitions.
            for (int i = 0; i < 4; i++) {
                mutable.value = iface("main.T" + i, "d" + i);
                assertEquals((long) (i + 1), shared.executeLong(f),
                        "megamorphic re-dispatch " + i);
            }
            assertEquals(1, IfaceDispatchNode.megamorphicTransitions(),
                    "no further transitions on megamorphic hits");
            // Each type reached its own method func.
            for (int i = 0; i < 4; i++) {
                assertEquals(2, roots[i].seen.size(), "method " + i + " invoked twice");
            }
        } finally {
            tearDown();
        }
    }

    /** Mutable receiver for the shared-site overflow test. */
    static final class MutableRecv extends GoExprNode {
        GoInterface value;

        MutableRecv(GoInterface value) {
            super(GoKind.INTERFACE);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    /**
     * Nil interface (Java null and descriptor-less GoInterface) panics with
     * gc's nil-dereference message — parity with FuncValueCallNode's nil-func
     * path (both use GoErrors.nilDereference()).
     * verifies: GG-0014
     */
    public void testNilPanics() {
        CaptureRoot root = new CaptureRoot(0L);
        setUp(Map.of("main.T", Map.of("M", target(root))));
        try {
            String want = GoErrors.nilDereference().errorString();
            assertEquals("runtime error: invalid memory address or nil pointer dereference",
                    want, "expected gc message text");
            // Java-null receiver (nil interface zero value).
            IfaceDispatchNode n1 = node(new ConstNode(GoKind.INTERFACE, null, 0),
                    new GoExprNode[0], List.of("M"), 3);
            assertNilPanic(n1, want, "java-null receiver");
            // Descriptor-less GoInterface (GoInterface.NIL).
            IfaceDispatchNode n2 = node(new ConstNode(GoKind.INTERFACE, GoInterface.NIL, 0),
                    new GoExprNode[0], List.of("M"), 3);
            assertNilPanic(n2, want, "GoInterface.NIL receiver");
            assertEquals(0, root.seen.size(), "method never invoked on nil receiver");
        } finally {
            tearDown();
        }
    }

    private static void assertNilPanic(IfaceDispatchNode n, String want, String msg) {
        try {
            n.executeObject(frame());
        } catch (PanicException pe) {
            assertTrue(pe.goValue() instanceof GoErrorValue, msg + ": panic value is a GoErrorValue");
            assertEquals(want, ((GoErrorValue) pe.goValue()).errorString(), msg + ": message parity");
            return;
        }
        throw new AssertionError(msg + ": expected PanicException");
    }

    /**
     * Typed nil (descriptor set, data null) proceeds; the method sees null
     * as params[0].
     * verifies: GG-0014
     */
    public void testTypedNilProceeds() {
        CaptureRoot root = new CaptureRoot(9L);
        setUp(Map.of("main.T", Map.of("M", target(root))));
        try {
            GoInterface typedNil = iface("main.T", null);
            assertTrue(!typedNil.isNil(), "test builds a typed nil");
            IfaceDispatchNode n = node(new ConstNode(GoKind.INTERFACE, typedNil, 0),
                    new GoExprNode[0], List.of("M"), 3);
            assertEquals(9L, n.executeLong(frame()), "typed-nil dispatch result");
            assertEquals(1, root.seen.size(), "method invoked once");
            assertEquals(null, root.seen.get(0)[1], "params[0] is null for typed nil");
        } finally {
            tearDown();
        }
    }

    /**
     * A method missing from the installed table fails fast with
     * FrontendException (unreachable for well-formed input: go/types
     * verified assignability).
     * verifies: GG-0014
     */
    public void testMissingMethodFailsFast() {
        setUp(Map.of("main.T", Map.of()));
        try {
            IfaceDispatchNode n = node(new ConstNode(GoKind.INTERFACE, iface("main.T", "d"), 0),
                    new GoExprNode[0], List.of("M"), 3);
            try {
                n.executeObject(frame());
            } catch (FrontendException fe) {
                assertTrue(fe.getMessage().contains("missing"),
                        "message names the missing method: " + fe.getMessage());
                return;
            }
            throw new AssertionError("expected FrontendException for missing method");
        } finally {
            tearDown();
        }
    }

    /**
     * No installed table for the descriptor fails fast.
     * verifies: GG-0014
     */
    public void testMissingTableFailsFast() {
        setUp(Map.of());
        try {
            IfaceDispatchNode n = node(new ConstNode(GoKind.INTERFACE, iface("main.T", "d"), 0),
                    new GoExprNode[0], List.of("M"), 3);
            try {
                n.executeObject(frame());
            } catch (FrontendException fe) {
                assertTrue(fe.getMessage().contains("no method table installed"),
                        "message names the missing table: " + fe.getMessage());
                return;
            }
            throw new AssertionError("expected FrontendException for missing table");
        } finally {
            tearDown();
        }
    }

    /**
     * Every dispatch records its itab with InterfaceDispatchProbes.
     * verifies: GG-0014
     */
    public void testProbesRecorded() {
        CaptureRoot root = new CaptureRoot(1L);
        setUp(Map.of("main.T", Map.of("M", target(root))));
        InterfaceDispatchProbes.setEnabled(true);
        try {
            IfaceDispatchNode n = node(new ConstNode(GoKind.INTERFACE, iface("main.T", "d"), 0),
                    new GoExprNode[0], List.of("M"), 3);
            VirtualFrame f = frame();
            n.executeObject(f);
            n.executeObject(f);
            Map<String, InterfaceDispatchProbes.Site> sites = InterfaceDispatchProbes.sites();
            assertEquals(1, sites.size(), "one probe site registered");
            InterfaceDispatchProbes.Site site =
                    sites.values().iterator().next();
            assertEquals(2, site.total(), "two dispatches recorded");
            assertEquals(1, site.histogram().size(), "one itab identity observed");
        } finally {
            tearDown();
        }
    }

    /**
     * Promoted method (recvPath [0]): the embedded field value — not the
     * outer struct — is passed as the method's params[0]. Red: without
     * receiver adaptation the outer struct reaches the method (the
     * end-to-end ClassCastException on embedded promotion).
     * verifies: GG-0014
     */
    public void testPromotedReceiverAdapted() {
        CaptureRoot root = new CaptureRoot(9L);
        setUp(Map.of("main.MW", Map.of("M", promotedTarget(root, 0))));
        try {
            graalgo.values.GoStructDescriptor desc = new graalgo.values.GoStructDescriptor("main.MW",
                    List.of(new graalgo.values.GoStructDescriptor.Field("Buf",
                            GoKind.POINTER, false, 0)));
            graalgo.values.GoStruct outer =
                    new graalgo.values.GoStruct(desc, new long[0], new Object[]{"EMBEDDED"});
            GoExprNode recv = new ConstNode(GoKind.INTERFACE, iface("main.MW", outer), 0);
            IfaceDispatchNode n = node(recv, new GoExprNode[0], List.of("M"), 3);
            VirtualFrame f = frame();
            assertEquals(9L, n.executeLong(f), "promoted dispatch result");
            assertEquals(1, root.seen.size(), "method invoked once");
            assertEquals("EMBEDDED", root.seen.get(0)[1],
                    "params[0] is the embedded field value, not the outer struct");
        } finally {
            tearDown();
        }
    }

    /**
     * Direct method (empty recvPath): the receiver's data passes through
     * unchanged — the adaptation walk is a no-op.
     * verifies: GG-0014
     */
    public void testDirectReceiverUnchanged() {
        CaptureRoot root = new CaptureRoot(3L);
        setUp(Map.of("main.T", Map.of("M", target(root))));
        try {
            GoExprNode recv = new ConstNode(GoKind.INTERFACE, iface("main.T", "DATA"), 0);
            IfaceDispatchNode n = node(recv, new GoExprNode[0], List.of("M"), 3);
            n.executeLong(frame());
            assertEquals("DATA", root.seen.get(0)[1], "params[0] is the receiver data");
        } finally {
            tearDown();
        }
    }
}
