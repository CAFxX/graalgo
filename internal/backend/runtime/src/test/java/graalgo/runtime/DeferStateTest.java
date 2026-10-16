package graalgo.runtime;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.atomic.AtomicReference;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.DeferNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoFuncRootNode;
import graalgo.frontend.GoStmtNode;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * TR-8: goroutine-state semantics for Java-driven Go invocations.
 *
 * <p>Decided semantics (see {@link GoCalls#invoke}): every entry into
 * compiled Go code runs with a non-null {@link GoroutineState}. Calls from
 * Java-driven threads that carry no state (finalizers, pool cleanup,
 * Java-side {@link Linknames#callGo}, reflect calls from Java) install a
 * fresh thread-confined state for the call and restore the previous holder
 * afterwards; on-goroutine calls reuse the goroutine's own state.
 *
 * <p>These tests hand-build a tiny Truffle AST — an outer Go function whose
 * body runs a {@code defer} of an inner recorder function — and drive it
 * through {@link Linknames#callGo} (which funnels through
 * {@link GoCalls#invoke}), asserting the deferred call actually runs.
 * Before the fix, the stateless-thread cases died with a Java
 * {@code NullPointerException} in {@code DeferNode} (or silently dropped
 * defers in {@code GoFuncRootNode}); every test below fails on that
 * behavior.
 *
 * <p>verifies: TR-8
 */
public final class DeferStateTest {

    /** Fired deferred-cleanup markers (test-body observable). */
    private static final List<String> FIRED = Collections.synchronizedList(new ArrayList<>());
    /** GoroutineState observed inside the deferred cleanup. */
    private static volatile GoroutineState seenInDeferred;
    /** GoroutineState observed inside the function body. */
    private static volatile GoroutineState seenInBody;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    // ------------------------------------------------------------------
    // Tiny hand-built AST.
    // ------------------------------------------------------------------

    /** Runs a fixed list of statements in order. */
    private static final class Seq extends GoStmtNode {
        @Children private final GoStmtNode[] stmts;

        Seq(GoStmtNode... stmts) {
            this.stmts = stmts;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            for (GoStmtNode s : stmts) {
                s.executeVoid(frame);
            }
        }
    }

    /** Runs a Runnable as a statement (test seam). */
    private static GoStmtNode stmt(Runnable r) {
        return new GoStmtNode() {
            @Override
            public void executeVoid(VirtualFrame frame) {
                r.run();
            }
        };
    }

    /** A constant expression node (test seam). */
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

    /** Builds a void, parameterless GoFunc running {@code body}. */
    private static GoFunc goFunc(String name, GoStmtNode body) {
        FrameDescriptor.Builder b = FrameDescriptor.newBuilder();
        int closureSlot = b.addSlot(FrameSlotKind.Object, "$closure", null);
        GoFuncRootNode root = new GoFuncRootNode(b.build(), body, closureSlot,
                new int[0], new GoKind[0], -1, null, name);
        RootCallTarget target = root.getCallTarget();
        return new GoFunc(target, null);
    }

    /**
     * Binds {@code name} to a Go function whose body records the current
     * state, defers a recorder, and optionally panics: the full
     * defer-via-callGo path in one AST.
     */
    private static void bindHelper(String name, boolean panics) {
        GoFunc cleanup = goFunc(name + ".cleanup", stmt(() -> {
            seenInDeferred = GoroutineState.current();
            FIRED.add("cleanup");
        }));
        GoStmtNode body = new Seq(
                stmt(() -> seenInBody = GoroutineState.current()),
                new DeferNode(new Const(GoKind.FUNC, cleanup),
                        new GoExprNode[0], null, new GoKind[0]),
                panics ? stmt(() -> Panic.throwPanic("boom", null, "string", null))
                       : stmt(() -> {}));
        Linknames.bindGo(name, goFunc(name, body));
    }

    /** Runs {@code r} on a fresh thread and returns any throwable it raised. */
    private static Throwable runOnFreshThread(Runnable r) throws InterruptedException {
        AtomicReference<Throwable> err = new AtomicReference<>();
        Thread t = new Thread(() -> {
            try {
                r.run();
            } catch (Throwable th) {
                err.set(th);
            }
        }, "deferstate-test");
        t.start();
        t.join();
        return err.get();
    }

    // ------------------------------------------------------------------
    // Tests.
    // ------------------------------------------------------------------

    /**
     * A {@code defer} reached via {@link Linknames#callGo} on a thread with
     * no GoroutineState runs its deferred call (fresh state installed),
     * and the thread is stateless again afterwards (no leak).
     */
    public void testDeferRunsViaCallGoWithoutState() throws Exception {
        FIRED.clear();
        seenInBody = null;
        seenInDeferred = null;
        bindHelper("main.deferHelper", false);

        Throwable err = runOnFreshThread(() -> {
            assertTrue(GoroutineState.current() == null,
                    "test thread must start with no GoroutineState");
            Linknames.callGo("main.deferHelper", new Object[0]);
            assertTrue(GoroutineState.current() == null,
                    "fresh state must be restored (no leak) after callGo");
        });

        assertTrue(err == null, "callGo on a stateless thread must not throw: " + err);
        assertTrue(FIRED.equals(List.of("cleanup")),
                "deferred cleanup must have run exactly once, got: " + FIRED);
        assertTrue(seenInBody != null, "body must have observed a GoroutineState");
        assertTrue(seenInDeferred == seenInBody,
                "deferred call must run under the same installed state as the body");
    }

    /**
     * A {@code defer} reached via {@link Linknames#callGo} on a thread that
     * already has a state reuses it (never shadows a goroutine's state) and
     * unwinds its defers instead of leaking them onto the caller's chain.
     */
    public void testCallGoReusesExistingState() {
        FIRED.clear();
        seenInBody = null;
        bindHelper("main.deferHelper2", false);

        GoroutineState outer = new GoroutineState();
        int baseDepth = outer.deferChain().depth();
        GoroutineState prev = GoroutineState.install(outer);
        try {
            Linknames.callGo("main.deferHelper2", new Object[0]);
            assertTrue(GoroutineState.current() == outer,
                    "callGo must reuse the existing state, not install a fresh one");
            assertTrue(outer.deferChain().depth() == baseDepth,
                    "defers must unwind at helper return, not leak onto the caller's chain");
        } finally {
            GoroutineState.restore(prev);
        }
        assertTrue(FIRED.equals(List.of("cleanup")),
                "deferred cleanup must have run, got: " + FIRED);
        assertTrue(seenInBody == outer, "body must have seen the caller's state");
    }

    /**
     * A panic in a stateless-thread call unwinds the installed defers and
     * propagates as PanicException (not NullPointerException), with the
     * thread stateless afterwards.
     */
    public void testPanicUnwindsDefersWithoutState() throws Exception {
        FIRED.clear();
        bindHelper("main.panicHelper", true);

        Throwable err = runOnFreshThread(() -> {
            try {
                Linknames.callGo("main.panicHelper", new Object[0]);
                throw new AssertionError("expected PanicException to propagate");
            } catch (PanicException expected) {
                // The panic unwinds defers, then propagates: Go parity.
            }
            assertTrue(GoroutineState.current() == null,
                    "state must be restored even when the call panics");
        });

        assertTrue(err == null, "unexpected throwable: " + err);
        assertTrue(FIRED.equals(List.of("cleanup")),
                "deferred cleanup must run during panic unwind, got: " + FIRED);
    }

    /**
     * The entry invariant is enforced loudly: invoking a Go root target
     * directly — bypassing {@link GoCalls#invoke} — on a stateless thread
     * fails with a FrontendException naming the missing GoroutineState,
     * never a silent defer drop.
     */
    public void testRootNodeFailsLoudWithoutState() throws Exception {
        bindHelper("main.loudHelper", false);
        GoFunc fn = Linknames.resolveGo("main.loudHelper");
        assertTrue(fn != null, "helper must be bound");

        Throwable err = runOnFreshThread(() -> {
            try {
                fn.target().call(GoCalls.callArgs(fn, new Object[0]));
                throw new AssertionError("expected FrontendException for missing GoroutineState");
            } catch (FrontendException e) {
                assertTrue(e.getMessage().contains("GoroutineState"),
                        "error must name the missing state: " + e.getMessage());
            }
        });

        assertTrue(err == null, "unexpected throwable: " + err);
    }
}
