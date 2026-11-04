package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import java.util.ArrayList;
import java.util.List;

/**
 * Regression tests for REVIEW-2026-11-04-11 NEW-P1-4 / NEW-P1-5
 * (R11-DEFER): {@code defer} evaluation order and nil func values.
 *
 * <p>These pin {@link DeferNode} directly, because the Go-level shape
 * that would expose the evaluation order end to end —
 * {@code defer sideFn()(sideArg())} — is rejected by the frontend
 * before it reaches DeferNode ({@code buildDefer}: "cannot defer call
 * to call (WS D)"), and every defer shape the frontend does accept
 * (ident, funclit, methodref) evaluates the function value without a
 * user-visible side effect. The order bug is nevertheless real by
 * inspection (the argument loop ran before {@code fn.executeObject})
 * and becomes observable the moment the frontend widens the accepted
 * shapes, so it is pinned here at the node level. The nil-func
 * behavior is additionally pinned end to end by the Go regression
 * package {@code test/regression/defer-eval-order}.
 *
 * <p>RED (pre-fix): {@code testDeferEvaluatesFnBeforeArgs} fails —
 * the log is {@code [arg1, arg2, fn]} and {@code executeVoid} then
 * throws; {@code testDeferNilFuncPanicsAtInvokeNotAtDefer} fails —
 * {@code executeVoid} itself throws instead of pushing.
 * GREEN (post-fix): both pass.
 */
public final class DeferNodeTest {

    /** A GoExprNode that records its evaluation and returns a canned value. */
    private static final class RecordingNode extends GoExprNode {
        private final List<String> log;
        private final String name;
        private final Object value;

        RecordingNode(List<String> log, String name, Object value) {
            super(GoKind.STRING);
            this.log = log;
            this.name = name;
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            log.add(name);
            return value;
        }
    }

    /**
     * NEW-P1-4: the deferred function value is evaluated before its
     * arguments (ordinary call evaluation order).
     */
    public void testDeferEvaluatesFnBeforeArgs() {
        GoroutineState state = new GoroutineState();
        GoroutineState prev = GoroutineState.install(state);
        try {
            List<String> log = new ArrayList<>();
            GoExprNode fn = new RecordingNode(log, "fn", null);
            GoExprNode[] args = new GoExprNode[]{
                new RecordingNode(log, "arg1", "a1"),
                new RecordingNode(log, "arg2", "a2")};
            DeferNode n = new DeferNode(fn, args, null,
                    new GoKind[]{GoKind.STRING, GoKind.STRING});
            int mark = state.deferChain().depth();
            n.executeVoid(null);
            List<String> want = List.of("fn", "arg1", "arg2");
            if (!log.equals(want)) {
                throw new AssertionError(
                        "defer evaluation order = " + log + ", want " + want + " (NEW-P1-4)");
            }
            if (state.deferChain().depth() != mark + 1) {
                throw new AssertionError(
                        "defer did not push exactly one call (NEW-P1-4)");
            }
        } finally {
            GoroutineState.restore(prev);
        }
    }

    /**
     * NEW-P1-5: deferring a nil func value does not panic at the defer
     * statement; the call is pushed and panics with the
     * nil-dereference error when it runs at unwind.
     */
    public void testDeferNilFuncPanicsAtInvokeNotAtDefer() {
        GoroutineState state = new GoroutineState();
        GoroutineState prev = GoroutineState.install(state);
        try {
            List<String> log = new ArrayList<>();
            GoExprNode fn = new RecordingNode(log, "fn", null);
            DeferNode n = new DeferNode(fn, new GoExprNode[0], null, new GoKind[0]);
            int mark = state.deferChain().depth();
            // Must NOT throw: the nil call is pushed, not panicked here.
            n.executeVoid(null);
            if (state.deferChain().depth() != mark + 1) {
                throw new AssertionError(
                        "nil defer did not push exactly one call (NEW-P1-5)");
            }
            // The panic surfaces when the call runs at unwind.
            try {
                state.deferChain().unwindReturning(mark);
                throw new AssertionError(
                        "nil deferred call did not panic at unwind (NEW-P1-5)");
            } catch (PanicException p) {
                String v = String.valueOf(p.goValue());
                if (!v.contains("invalid memory address or nil pointer dereference")) {
                    throw new AssertionError(
                            "nil deferred call panic value = " + v
                                    + ", want nil-dereference (NEW-P1-5)");
                }
            }
        } finally {
            GoroutineState.restore(prev);
        }
    }
}
