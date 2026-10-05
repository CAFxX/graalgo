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
 * <p>Three forms: deferring a function value (a {@code funclit} or func-typed
 * variable), deferring a builtin call ({@code println}, {@code panic},
 * {@code recover} — the latter two for gc-exact unwind interplay), and
 * deferring a {@code fmt} overlay call (M7 — e.g. {@code defer
 * fmt.Println(x)}; the FmtCallNode's arguments are evaluated now, the
 * formatting replays at return/unwind time).
 */
public final class DeferNode extends GoStmtNode {
    @Child private GoExprNode fn;
    @Children private final GoExprNode[] args;
    /** Builtin name for deferred builtins; null for function values. */
    private final String builtinName;
    private final GoKind[] argKinds;
    /** Non-null only for a deferred {@code fmt} overlay call (M7). */
    @Child private FmtCallNode fmtCall;

    public DeferNode(GoExprNode fn, GoExprNode[] args, String builtinName, GoKind[] argKinds) {
        this.fn = fn;
        this.args = args;
        this.builtinName = builtinName;
        this.argKinds = argKinds;
    }

    /** A deferred {@code fmt} overlay call (M7). */
    public DeferNode(FmtCallNode fmtCall) {
        this.fmtCall = fmtCall;
        this.args = fmtCall.fmtArgs();
        this.builtinName = null;
        this.argKinds = fmtCall.argKinds();
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        DeferredCall dc;
        if (fmtCall != null) {
            // Fmt overlay call: evaluate arguments now (Go's call-time
            // evaluation), capture values plus static type metadata; the
            // formatting replays when the deferred call runs.
            GoExprNode[] fargs = fmtCall.fmtArgs();
            Object[] argVals = new Object[fargs.length];
            for (int i = 0; i < fargs.length; i++) {
                argVals[i] = FmtCallNode.readArg(fargs[i], frame);
            }
            dc = DeferredCall.fmtCall(fmtCall.funcName(), argVals, fmtCall.argKinds(),
                    fmtCall.argTypeNames(), fmtCall.argElemKinds(), fmtCall.argElemTypeNames(),
                    fmtCall.argElemKindChains(), fmtCall.argElemTypeNameChains(),
                    fmtCall.argKeyKinds(), fmtCall.argKeyTypeNames(),
                    fmtCall.argKeyKindChains(), fmtCall.argKeyTypeNameChains());
        } else {
            Object[] argVals = new Object[args.length];
            for (int i = 0; i < args.length; i++) {
                argVals[i] = Boxing.box(args[i].kind(), args[i], frame);
            }
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
        }
        GoroutineState.current().deferChain().push(dc);
    }
}
