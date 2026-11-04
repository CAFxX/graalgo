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
 * variable), and deferring a builtin call ({@code println}, {@code panic},
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
        {
            // NEW-P1-4 (REVIEW-2026-11-04-11): Go evaluates the deferred
            // function value BEFORE its arguments (ordinary call
            // evaluation order, gc-verified); the argument loop used to
            // run first, so a side effect in the function value ran
            // after the argument side effects. Builtin defers have no
            // function value; only their arguments are evaluated.
            GoFunc f = null;
            if (builtinName == null) {
                f = (GoFunc) fn.executeObject(frame);
            }
            Object[] argVals = new Object[args.length];
            for (int i = 0; i < args.length; i++) {
                // Go evaluates and copies defer arguments at the defer
                // statement (value semantics for arrays/structs).
                argVals[i] = Boxing.copyForAssign(args[i].kind(),
                        Boxing.box(args[i].kind(), args[i], frame));
            }
            if (builtinName != null) {
                dc = DeferredCall.builtin(builtinName, argVals, argKinds);
                // P3-M4 remediation (P1-2): a deferred panic(x) throws at
                // unwind time; the site is the defer statement (gc's line).
                dc.setCallSite(this);
            } else {
                dc = DeferredCall.func(f, argVals);
                if (f == null) {
                    // NEW-P1-5 (REVIEW-2026-11-04-11): gc does NOT panic
                    // here. A nil func value is pushed like any other
                    // deferred call and panics with the nil-dereference
                    // error only when the call runs at unwind
                    // (DeferredCall.invoke), so statements after this
                    // defer still execute and defers pushed later still
                    // run first (LIFO). The call site is attached so
                    // the unwind-time panic is attributed to this
                    // defer statement, as the old defer-time panic was.
                    dc.setCallSite(this);
                } else if (CallSiteProbes.isEnabled()) {
                    // M9 (P1-2): attach the probe site so the deferred invocation
                    // is recorded in the polymorphism histogram.
                    CallSiteProbes.Site site = probeSite;
                    if (site == null) {
                        site = CallSiteProbes.registerSite(siteName());
                        probeSite = site;
                    }
                    dc.setProbeSite(site);
                }
            }
        }
        GoroutineState.current().deferChain().push(dc);
    }
}
