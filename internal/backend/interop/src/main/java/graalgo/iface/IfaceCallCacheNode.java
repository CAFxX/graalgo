package graalgo.iface;

import com.oracle.truffle.api.dsl.Cached;
import com.oracle.truffle.api.dsl.Specialization;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.DirectCallNode;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;

/**
 * Interface-dispatch inline cache (lane T0.B; SPEC §3.3: "PIC on itab
 * identity, limit ~3, megamorphic fallback to hash lookup").
 *
 * <p>Keyed on the receiver's dynamic type descriptor identity, which
 * determines the itab for this site's interface (descriptors are minted
 * once per Go type, and itabs are immutable — D6 — so an entry never
 * needs invalidation). Each entry caches the resolved direct target and a
 * {@link DirectCallNode} on its call target, so the compiler can inline
 * the concrete method body at the interface call site (the
 * devirtualization cascade, SPEC §2.6.1). A target that is not a plain
 * direct method (promoted through embedding, embedded-interface
 * redispatch, implicit address-of) caches {@code null} and that type
 * always takes {@link IfaceDispatchNode#dispatchSlow}; so do all types
 * once the site has seen more than three (megamorphic).
 */
@SuppressWarnings("truffle-inlining")
public abstract class IfaceCallCacheNode extends Node {

    abstract Object execute(VirtualFrame frame, GoInterface gi, Object[] argVals,
            IfaceDispatchNode owner);

    @Specialization(limit = "3", guards = "gi.descriptor() == cachedDesc")
    static Object doCached(VirtualFrame frame, GoInterface gi, Object[] argVals,
                           IfaceDispatchNode owner,
                           @Cached("gi.descriptor()") Object cachedDesc,
                           @Cached("owner.resolveFast(gi)") IfaceDispatchNode.FastTarget fast,
                           @Cached("createCall(fast)") DirectCallNode call) {
        if (fast == null) {
            return owner.dispatchSlow(frame, gi, argVals);
        }
        return owner.callFast(call, fast, gi, argVals);
    }

    @Specialization(replaces = "doCached")
    static Object doMegamorphic(VirtualFrame frame, GoInterface gi, Object[] argVals,
            IfaceDispatchNode owner) {
        return owner.dispatchSlow(frame, gi, argVals);
    }

    static DirectCallNode createCall(IfaceDispatchNode.FastTarget fast) {
        return fast == null ? null : DirectCallNode.create(fast.func().target());
    }
}
