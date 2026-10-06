package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoroutineState;
import graalgo.values.CallSiteProbes;
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
    /** True when built from a {@code methodref} call: args[0] is the receiver. */
    private final boolean isMethodCall;
    /**
     * M9 (P1-1): enclosing funcdecl's object id. Each monomorphized
     * instantiation is a separate funcdecl (own objId) whose call nodes
     * re-use the origin's source positions; the site name includes this so
     * per-instantiation sites do not merge. -1 when unknown (tests).
     */
    private final int enclosingFuncObj;
    /** M9 polymorphism probe site, created lazily on first probed call. */
    private volatile CallSiteProbes.Site probeSite;

    public FuncValueCallNode(GoExprNode fn, GoExprNode[] args, GoKind resultKind) {
        this(fn, args, resultKind, false, -1);
    }

    public FuncValueCallNode(GoExprNode fn, GoExprNode[] args, GoKind resultKind,
            boolean isMethodCall) {
        this(fn, args, resultKind, isMethodCall, -1);
    }

    public FuncValueCallNode(GoExprNode fn, GoExprNode[] args, GoKind resultKind,
            boolean isMethodCall, int enclosingFuncObj) {
        super(resultKind);
        this.fn = fn;
        this.args = args;
        this.isMethodCall = isMethodCall;
        this.enclosingFuncObj = enclosingFuncObj;
    }

    private String siteName() {
        String pos;
        var section = getSourceSection();
        if (section == null) {
            pos = "unknown";
        } else {
            pos = section.getSource().getName() + ":" + section.getStartLine()
                    + ":" + section.getStartColumn();
        }
        return enclosingFuncObj + "@" + pos;
    }

    private void probe(GoFunc f, Object[] argVals) {
        if (!CallSiteProbes.isEnabled()) {
            return;
        }
        CallSiteProbes.Site site = probeSite;
        if (site == null) {
            site = CallSiteProbes.registerSite(siteName());
            probeSite = site;
        }
        // The compiled target (not the GoFunc wrapper: closure evaluations
        // allocate fresh wrappers around the same target) is the stable
        // polymorphism key — "how many distinct function bodies can this
        // site invoke".
        site.recordTarget(f.target());
        if (isMethodCall && argVals.length > 0) {
            site.recordReceiver(CallSiteProbes.receiverKey(argVals[0]));
        }
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
            probe(f, argVals);
            // P3-M4 (WS C): record the call site in the caller's frame
            // before the callee pushes its own — gc's traceback shows the
            // call site in each caller frame.
            GoroutineState callState = GoroutineState.current();
            if (callState != null) {
                callState.updateTopFrameSection(getSourceSection());
            }
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
