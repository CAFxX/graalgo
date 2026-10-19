package graalgo.frontend;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.DirectCallNode;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.values.GoFunc;

/**
 * RV22-P1-3: direct unit tests for {@link CallDispatchNode}'s polymorphic
 * inline cache.
 *
 * <p>The cache guard keys on {@code RootCallTarget} identity, not the
 * {@code GoFunc} wrapper. That is correct only because the cached
 * specialization re-derives the call arguments from the <em>call's</em>
 * wrapper on every hit ({@code GoCalls.callArgs(fn, args)}, which prepends
 * {@code fn.boundArgs()}). If a future change cached the wrapper or a
 * target were reused across semantically different functions, calls would
 * silently dispatch with the wrong receiver. These tests pin the contract:
 *
 * <ul>
 *   <li>(a) monomorphic site: two wrappers around one target share one
 *       cache entry and both dispatch correctly;
 *   <li>(b) the same target with different {@code boundArgs} (method
 *       values on different receivers) dispatches per-receiver through a
 *       polymorphic site;
 *   <li>(c) a fourth distinct target overflows the 3-entry PIC and takes
 *       the megamorphic fallback (cache entries dropped);
 *   <li>(d) results are correct on every path, including previously cached
 *       targets after the megamorphic transition and a never-seen fifth
 *       target.
 * </ul>
 *
 * <p>The {@code DirectCallNode} descendant count is the observable for
 * cache occupancy: each installed specialization holds one, and the
 * megamorphic transition drops the whole cache chain.
 */
public final class CallDispatchNodeTest {

    /**
     * A root whose body echoes its tag plus every argument after the
     * {@code GoFunc} wrapper ({@code frame.getArguments()[0]} is the
     * wrapper per {@code GoCalls.callArgs}; the rest are
     * {@code boundArgs} followed by the call arguments).
     */
    static final class EchoRoot extends RootNode {
        private final String tag;

        EchoRoot(String tag) {
            super(null);
            this.tag = tag;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Object[] a = frame.getArguments();
            StringBuilder sb = new StringBuilder(tag);
            for (int i = 1; i < a.length; i++) {
                sb.append(':').append(a[i]);
            }
            return sb.toString();
        }
    }

    /**
     * Adopts the dispatch node as a {@code @Child}, exactly as
     * {@link FuncValueCallNode} does in production.
     */
    static final class Harness extends Node {
        @Child CallDispatchNode dispatch;

        Harness() {
            dispatch = CallDispatchNodeGen.create();
        }
    }

    private static void assertEq(Object expected, Object actual, String what) {
        if (!expected.equals(actual)) {
            throw new AssertionError(
                    what + ": expected <" + expected + "> but got <" + actual + ">");
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    /** Number of installed cache entries, observed via DirectCallNode children. */
    private static int directCallNodes(Node root) {
        int n = 0;
        for (Node child : root.getChildren()) {
            if (child instanceof DirectCallNode) {
                n++;
            }
            n += directCallNodes(child);
        }
        return n;
    }

    public void testMonomorphicHitKeysOnTargetNotWrapper() {
        Harness h = new Harness();
        RootCallTarget target = new EchoRoot("A").getCallTarget();
        // Two fresh wrappers around the SAME target (closure evaluations
        // allocate fresh wrappers): the guard keys on target identity, so
        // both must share a single cache entry.
        GoFunc f1 = new GoFunc(target, null);
        GoFunc f2 = new GoFunc(target, null);
        assertEq("A:x", h.dispatch.executeCall(f1, new Object[]{"x"}),
                "first call installs the entry");
        assertEq("A:y", h.dispatch.executeCall(f2, new Object[]{"y"}),
                "second wrapper hits the same entry");
        assertEq("A:z", h.dispatch.executeCall(f1, new Object[]{"z"}),
                "first wrapper still dispatches");
        assertEq(1, directCallNodes(h.dispatch),
                "one cache entry for two wrappers of one target");
    }

    public void testSameTargetDifferentBoundArgsDispatchesPerReceiver() {
        Harness h = new Harness();
        // One method body (one RootCallTarget), two method values bound to
        // different receivers: the cached specialization must re-derive
        // boundArgs from the *call's* wrapper on every hit (GoCalls.callArgs),
        // never reuse a stale receiver. A wrapper-keyed or boundArgs-caching
        // implementation returns the first receiver's result for the second
        // call — this test is red under that bug.
        RootCallTarget methodTarget = new EchoRoot("M").getCallTarget();
        GoFunc recv1 = new GoFunc(methodTarget, null, null, new Object[]{"r1"});
        GoFunc recv2 = new GoFunc(methodTarget, null, null, new Object[]{"r2"});
        assertEq("M:r1:v", h.dispatch.executeCall(recv1, new Object[]{"v"}),
                "receiver 1 dispatches");
        assertEq("M:r2:v", h.dispatch.executeCall(recv2, new Object[]{"v"}),
                "receiver 2: same target, different boundArgs");
        assertEq("M:r1:w", h.dispatch.executeCall(recv1, new Object[]{"w"}),
                "receiver 1 still dispatches after interleaving");
        assertEq("M:r2:w", h.dispatch.executeCall(recv2, new Object[]{"w"}),
                "receiver 2 still dispatches after interleaving");
        assertEq(1, directCallNodes(h.dispatch),
                "one entry: guard is target identity, not the wrapper");
    }

    public void testPolymorphicSiteThenMegamorphicFallback() {
        Harness h = new Harness();
        RootCallTarget tA = new EchoRoot("A").getCallTarget();
        RootCallTarget tB = new EchoRoot("B").getCallTarget();
        RootCallTarget tC = new EchoRoot("C").getCallTarget();
        RootCallTarget tD = new EchoRoot("D").getCallTarget();
        RootCallTarget tE = new EchoRoot("E").getCallTarget();
        GoFunc a1 = new GoFunc(tA, null, null, new Object[]{"r1"});
        GoFunc a2 = new GoFunc(tA, null, null, new Object[]{"r2"});
        GoFunc b = new GoFunc(tB, null);
        GoFunc c = new GoFunc(tC, null);
        GoFunc d = new GoFunc(tD, null);
        GoFunc e = new GoFunc(tE, null);

        // Three distinct targets fill the PIC (limit = 3): target A shared
        // by two receivers plus B and C.
        assertEq("A:r1:1", h.dispatch.executeCall(a1, new Object[]{"1"}), "A/r1");
        assertEq("A:r2:2", h.dispatch.executeCall(a2, new Object[]{"2"}),
                "A/r2: same target, other receiver");
        assertEq("B:3", h.dispatch.executeCall(b, new Object[]{"3"}), "B");
        assertEq("C:4", h.dispatch.executeCall(c, new Object[]{"4"}), "C");
        assertEq(3, directCallNodes(h.dispatch), "three cache entries installed");

        // A fourth distinct target overflows the PIC: the node must take the
        // megamorphic fallback (GoCalls.invoke), dropping the cache entries.
        assertEq("D:5", h.dispatch.executeCall(d, new Object[]{"5"}),
                "D via megamorphic fallback");
        assertEq(0, directCallNodes(h.dispatch),
                "megamorphic: no cached DirectCallNodes remain");
        assertTrue(h.dispatch.getClass() == CallDispatchNodeGen.class,
                "fallback rewrites state in place (same generated node class)");

        // Every earlier target still dispatches correctly through the
        // fallback, and so does a fifth, never-seen target: the fallback
        // handles unbounded polymorphism without re-specializing.
        assertEq("A:r1:6", h.dispatch.executeCall(a1, new Object[]{"6"}),
                "A/r1 after megamorphic");
        assertEq("A:r2:7", h.dispatch.executeCall(a2, new Object[]{"7"}),
                "A/r2 after megamorphic");
        assertEq("B:8", h.dispatch.executeCall(b, new Object[]{"8"}),
                "B after megamorphic");
        assertEq("C:9", h.dispatch.executeCall(c, new Object[]{"9"}),
                "C after megamorphic");
        assertEq("D:10", h.dispatch.executeCall(d, new Object[]{"10"}),
                "D after megamorphic");
        assertEq("E:11", h.dispatch.executeCall(e, new Object[]{"11"}),
                "unbounded polymorphism via fallback");
        assertEq(0, directCallNodes(h.dispatch), "fallback does not re-specialize");
    }
}
