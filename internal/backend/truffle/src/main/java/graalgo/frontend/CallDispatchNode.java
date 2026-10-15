package graalgo.frontend;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.dsl.Cached;
import com.oracle.truffle.api.dsl.Fallback;
import com.oracle.truffle.api.dsl.Specialization;
import com.oracle.truffle.api.nodes.DirectCallNode;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;

/**
 * Per-call-site dispatch for Go function values (Truffle DSL).
 *
 * <p>Replaces the unconditional megamorphic {@code RootCallTarget.call()}
 * in {@link GoCalls#invoke} with a polymorphic inline cache: up to three
 * distinct targets each get a {@link DirectCallNode}, which the Graal
 * compiler can inline at the call site. A fourth distinct target rewrites
 * the node to the megamorphic {@link #callMegamorphic fallback}, which
 * keeps today's exact behavior (indirect call through
 * {@link GoCalls#invoke}).
 *
 * <p>The guard keys on the compiled {@link RootCallTarget} (not the
 * {@link GoFunc} wrapper): closure evaluations allocate fresh wrappers
 * around the same target, so target identity is the stable polymorphism
 * key (same rationale as {@code CallSiteProbes}).
 */
// The node-object-inlining decision is deferred (matches the DslSmoke
// convention); suppress the processor's candidacy hint at the source.
@SuppressWarnings("truffle-inlining")
public abstract class CallDispatchNode extends Node {

    /** Evaluates the call through this node's inline cache. */
    public abstract Object executeCall(GoFunc fn, Object[] args);

    @Specialization(limit = "3", guards = "fn.target() == cachedTarget")
    static Object callCached(GoFunc fn, Object[] args,
                             @Cached("fn.target()") RootCallTarget cachedTarget,
                             @Cached("create(cachedTarget)") DirectCallNode callNode) {
        return callNode.call(GoCalls.callArgs(fn, args));
    }

    @Fallback
    static Object callMegamorphic(GoFunc fn, Object[] args) {
        return GoCalls.invoke(fn, args);
    }
}
