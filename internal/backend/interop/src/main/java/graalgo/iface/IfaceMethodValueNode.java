package graalgo.iface;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * An interface method value {@code w.M} in value position (P1-9,
 * review-2026-10-31-22): the frontend {@code "ifacemethodvalue"} wire
 * node.
 *
 * <p>Evaluates the receiver expression once at creation time (Go
 * semantics: the receiver is evaluated and saved when the method value
 * is formed) and produces a {@link GoFunc} whose target is the
 * per-site {@link IfaceMethodValueRootNode}: every invocation of the
 * resulting function value dispatches through the itab for the bound
 * interface value's dynamic type, exactly like a direct interface call
 * made at formation time would — and, unlike a closure over the
 * receiver variable, a later reassignment of that variable does not
 * change what the value calls.
 *
 * <p>Forming the value never panics on a nil interface: the bound value
 * is carried as-is (Java null or a descriptor-less {@code GoInterface})
 * and only a call through it panics, with gc's nil-dereference panic,
 * via the dispatcher's receiver check — matching gc, where
 * {@code f := nilIface.M} succeeds and {@code f()} panics.
 *
 * <p>Multi-result methods need no special case here: the itab target
 * for such a method is its pair-returning form (as for direct
 * ifacemethod calls), so the returned {@code GoFunc} yields the pair
 * struct and call sites through a variable holding it desugar via the
 * frontend's pair info, exactly like a direct call.
 */
public final class IfaceMethodValueNode extends GoExprNode {
    @Child private GoExprNode recv;
    private final RootCallTarget target;

    public IfaceMethodValueNode(GoExprNode recv, RootCallTarget target) {
        super(GoKind.FUNC);
        this.recv = recv;
        this.target = target;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object recvVal = recv.executeObject(frame);
        return new GoFunc(target, null, null, new Object[] {recvVal});
    }
}
