package graalgo.iface;

import java.util.Arrays;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;

import graalgo.safepoint.Safepoint;
import graalgo.values.InterfaceDispatchProbes;

/**
 * Call target for an interface method value (P1-9,
 * review-2026-10-31-22): the {@link GoFunc} produced by
 * {@link IfaceMethodValueNode} carries the bound interface value as its
 * single bound argument, so the calling convention (see
 * {@link graalgo.runtime.GoCalls#callArgs}) delivers
 * {@code args[0] = GoFunc, args[1] = bound interface value,
 * args[2..] = call arguments}. This root dispatches through the shared
 * {@link IfaceDispatcher} on each call — the dynamic type is read from
 * the bound value at call time, not from the static interface type, so
 * the value behaves exactly like the interface call it was formed from.
 *
 * <p>The root carries no frame slots: it holds no Go locals, and the
 * callee's own root (reached via {@code GoCalls.invoke}) owns its
 * frame. Like {@link graalgo.frontend.LinknameRootNode}, no Go stack
 * frame is pushed for the dispatch itself and no defer chain runs here
 * (the callee runs its own).
 */
public final class IfaceMethodValueRootNode extends RootNode {
    private final IfaceDispatcher dispatcher;
    private final int enclosingFuncObj;
    private SourceSection sourceSection;

    public IfaceMethodValueRootNode(IfaceDispatcher dispatcher, int enclosingFuncObj) {
        super((TruffleLanguage<?>) null, FrameDescriptor.newBuilder().build());
        this.dispatcher = dispatcher;
        this.enclosingFuncObj = enclosingFuncObj;
    }

    // Plain method (cf. LinknameRootNode): Node has no setSourceSection
    // in this Truffle version; the builder calls this directly.
    public void setSourceSection(SourceSection sourceSection) {
        this.sourceSection = sourceSection;
    }

    @Override
    public SourceSection getSourceSection() {
        return sourceSection;
    }

    @Override
    public String getName() {
        return "ifacevalue:" + dispatcher.methodName();
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Safepoint.poll();
        Object[] args = frame.getArguments();
        // args[0] is the GoFunc itself (calling convention); args[1] is
        // the bound interface value; args[2..] are the call arguments.
        Object bound = args.length > 1 ? args[1] : null;
        Object[] callArgs = args.length > 2
                ? Arrays.copyOfRange(args, 2, args.length)
                : new Object[0];
        return dispatcher.dispatch(bound, callArgs, this, this::probe);
    }

    /** M9 polymorphism probe site, created lazily on first probed dispatch. */
    private volatile InterfaceDispatchProbes.Site probeSite;

    private void probe(IfaceItab itab) {
        if (!InterfaceDispatchProbes.isEnabled()) {
            return;
        }
        InterfaceDispatchProbes.Site site = probeSite;
        if (site == null) {
            site = InterfaceDispatchProbes.registerSite(siteName());
            probeSite = site;
        }
        site.record(itab);
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
        return "ifacevalue:" + enclosingFuncObj + "@" + pos + "#" + dispatcher.methodName();
    }

    @Override
    public String toString() {
        return "IfaceMethodValueRootNode{" + dispatcher.methodName() + "}";
    }
}
