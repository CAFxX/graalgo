package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoroutineState;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * A call to a Go function value (M5, WS F).
 *
 * <p>Evaluates the function value and arguments, then invokes via
 * {@link GoCalls}. A nil function value panics with gc's nil-dereference
 * panic. The recover-allowed flag is cleared for the duration of the call:
 * {@code recover()} must be called <em>directly</em> by a deferred
 * function, so nested calls (even from a deferred function) see nil
 * (gc-verified).
 */
public final class FuncValueCallNode extends GoExprNode {
    @Child private GoExprNode fn;
    @Children private final GoExprNode[] args;

    public FuncValueCallNode(GoExprNode fn, GoExprNode[] args, GoKind resultKind) {
        super(resultKind);
        this.fn = fn;
        this.args = args;
    }

    private Object invoke(VirtualFrame frame) {
        GoFunc f = (GoFunc) fn.executeObject(frame);
        if (f == null) {
            graalgo.runtime.Panic.throwPanic(graalgo.runtime.GoErrors.nilDereference());
        }
        Object[] argVals = new Object[args.length];
        for (int i = 0; i < args.length; i++) {
            argVals[i] = Boxing.box(args[i].kind(), args[i], frame);
        }
        // recover() is only non-nil when called directly by a deferred
        // function: clear the flag for nested calls, restore afterwards.
        GoroutineState state = GoroutineState.current();
        boolean prev = state != null && state.isRecoverAllowed();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        try {
            return GoCalls.invoke(f, argVals);
        } finally {
            if (state != null) {
                state.setRecoverAllowed(prev);
            }
        }
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return invoke(frame);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object r = invoke(frame);
        return r == null ? 0L : ((Number) r).longValue();
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) invoke(frame)).intValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Number) invoke(frame)).doubleValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return (Boolean) invoke(frame);
    }
}
