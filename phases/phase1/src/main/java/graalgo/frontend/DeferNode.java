package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.DeferredCall;
import graalgo.runtime.GoroutineState;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * A {@code defer} statement (M5, WS F).
 *
 * <p>Evaluates the function value and arguments <em>now</em> (Go's
 * call-time evaluation), pushes the {@link DeferredCall} onto the current
 * goroutine's {@link graalgo.runtime.DeferChain}, and continues. The call
 * runs LIFO when the surrounding function returns, panics, or goexits.
 *
 * <p>Two forms: deferring a function value (a {@code funclit} or func-typed
 * variable) and deferring a builtin call ({@code println}, {@code panic},
 * {@code recover} — the latter two for gc-exact unwind interplay).
 */
public final class DeferNode extends GoStmtNode {
    @Child private GoExprNode fn;
    @Children private final GoExprNode[] args;
    /** Builtin name for deferred builtins; null for function values. */
    private final String builtinName;
    private final GoKind[] argKinds;

    public DeferNode(GoExprNode fn, GoExprNode[] args, String builtinName, GoKind[] argKinds) {
        this.fn = fn;
        this.args = args;
        this.builtinName = builtinName;
        this.argKinds = argKinds;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object[] argVals = new Object[args.length];
        for (int i = 0; i < args.length; i++) {
            argVals[i] = Boxing.box(args[i].kind(), args[i], frame);
        }
        DeferredCall dc;
        if (builtinName != null) {
            dc = DeferredCall.builtin(builtinName, argVals, argKinds);
        } else {
            GoFunc f = (GoFunc) fn.executeObject(frame);
            if (f == null) {
                // Deferring a nil func value: gc panics at defer time with
                // the nil-dereference panic.
                graalgo.runtime.Panic.throwPanic(graalgo.runtime.GoErrors.nilDereference());
            }
            dc = DeferredCall.func(f, argVals);
        }
        GoroutineState.current().deferChain().push(dc);
    }
}
