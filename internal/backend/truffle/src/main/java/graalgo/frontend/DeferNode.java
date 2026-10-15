package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.DeferredCall;
import graalgo.runtime.GoroutineState;
import graalgo.values.CallSiteProbes;
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
    /**
     * M9 (P1-2): enclosing funcdecl's object id for probe site disambiguation
     * (see FuncValueCallNode). Set by the factory; -1 when unknown.
     */
    private int enclosingFuncObj = -1;
    /** M9 polymorphism probe site, created lazily on first probed defer. */
    private volatile CallSiteProbes.Site probeSite;

    /** Sets the enclosing funcdecl's object id (called by NodeFactory). */
    public void setEnclosingFunc(int funcObjId) {
        this.enclosingFuncObj = funcObjId;
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

    public DeferNode(GoExprNode fn, GoExprNode[] args, String builtinName, GoKind[] argKinds) {
        this.fn = fn;
        this.args = args;
        this.builtinName = builtinName;
        this.argKinds = argKinds;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        DeferredCall dc;
        Object[] argVals = new Object[args.length];
        for (int i = 0; i < args.length; i++) {
            argVals[i] = Boxing.box(args[i].kind(), args[i], frame);
        }
        if (builtinName != null) {
            dc = DeferredCall.builtin(builtinName, argVals, argKinds);
            // P3-M4 remediation (P1-2): a deferred panic(x) throws at
            // unwind time; the site is the defer statement (gc's line).
            dc.setCallSite(this);
        } else {
            GoFunc f = (GoFunc) fn.executeObject(frame);
            if (f == null) {
                // Deferring a nil func value: gc panics at defer time with
                // the nil-dereference panic.
                graalgo.runtime.Panic.throwPanic(graalgo.runtime.GoErrors.nilDereference(), null, null, this);
            }
            dc = DeferredCall.func(f, argVals);
            // M9 (P1-2): attach the probe site so the deferred invocation
            // is recorded in the polymorphism histogram.
            if (CallSiteProbes.isEnabled()) {
                CallSiteProbes.Site site = probeSite;
                if (site == null) {
                    site = CallSiteProbes.registerSite(siteName());
                    probeSite = site;
                }
                dc.setProbeSite(site);
            }
        }
        GoroutineState.current().deferChain().push(dc);
    }
}
