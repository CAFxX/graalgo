package graalgo.iface;

import java.util.List;

import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.Boxing;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import graalgo.values.InterfaceDispatchProbes;

/**
 * Interface method dispatch (P3-M3, WS D, ADR-0003): the
 * {@code "ifacemethod"} wire node.
 *
 * <p>The receiver evaluates to a {@link graalgo.values.GoInterface} (a nil
 * interface — Java null or a descriptor-less {@code GoInterface} — is gc's
 * nil-dereference panic, via the same {@code Panic.throwPanic} /
 * {@code GoErrors.nilDereference()} path {@code FuncValueCallNode} uses
 * for nil funcs). A typed nil (descriptor set, data null) proceeds with
 * null receiver data.
 *
 * <p>The dispatch itself lives in {@link IfaceDispatcher}, shared with
 * the {@code "ifacemethodvalue"} value node (P1-9): this node only
 * evaluates its receiver and argument children, boxes them, and hands
 * them to the dispatcher. Every dispatch records
 * {@code InterfaceDispatchProbes.record(itab)} (the M9 re-measurement
 * hook).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind("ifacemethod", ...)} hook — no
 * Phase-1 {@code NodeFactory} change.
 *
 * <p>verifies: GG-0014
 * <p>verifies: GG-0017 (iface slice)
 */
public final class IfaceDispatchNode extends GoExprNode {

    @Child
    private GoExprNode recv;
    @Children
    private GoExprNode[] args;

    private final IfaceDispatcher dispatcher;
    /**
     * Enclosing funcdecl's object id (probe site naming, cf.
     * {@code FuncValueCallNode}); -1 when unknown (tests).
     */
    private final int enclosingFuncObj;

    public IfaceDispatchNode(GoExprNode recv, GoExprNode[] args, String methodName,
            List<String> ifaceMethodNames, int ifaceId, GoKind resultKind, int enclosingFuncObj) {
        super(resultKind);
        this.recv = recv;
        this.args = args;
        this.dispatcher = new IfaceDispatcher(methodName, ifaceMethodNames, ifaceId);
        this.enclosingFuncObj = enclosingFuncObj;
    }

    /** Process-wide megamorphic-transition count (observability). */
    public static long megamorphicTransitions() {
        return IfaceDispatcher.megamorphicTransitions();
    }

    /** Resets the transition counter (tests). */
    static void resetMegamorphicTransitions() {
        IfaceDispatcher.resetMegamorphicTransitions();
    }

    private Object dispatch(VirtualFrame frame) {
        // Evaluation order (unchanged from before the P1-9 factoring):
        // the receiver evaluates before the arguments, as for a direct
        // method call.
        Object recvVal = recv.executeObject(frame);
        Object[] argVals = new Object[args.length];
        for (int i = 0; i < args.length; i++) {
            argVals[i] = boxArg(args[i], frame);
        }
        return dispatcher.dispatch(recvVal, argVals, this, this::probe);
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
        return "iface:" + enclosingFuncObj + "@" + pos + "#" + dispatcher.methodName();
    }

    /**
     * Reads an argument at its kind's slot kind (replica of the
     * package-private {@code Boxing.box}: canonical boxed values, e.g. all
     * int/uint widths as {@link Long}).
     */
    private static Object boxArg(GoExprNode arg, VirtualFrame frame) {
        FrameSlotKind slot = arg.kind().slotKind();
        if (slot == FrameSlotKind.Int) {
            return Integer.valueOf(arg.executeInt(frame));
        }
        if (slot == FrameSlotKind.Long) {
            return Long.valueOf(arg.executeLong(frame));
        }
        if (slot == FrameSlotKind.Float) {
            return Float.valueOf(arg.executeFloat(frame));
        }
        if (slot == FrameSlotKind.Double) {
            return Double.valueOf(arg.executeDouble(frame));
        }
        if (slot == FrameSlotKind.Boolean) {
            return Boolean.valueOf(arg.executeBoolean(frame));
        }
        if (slot == FrameSlotKind.Object) {
            return arg.executeObject(frame);
        }
        throw new FrontendException("cannot box kind " + arg.kind() + " (WS E)");
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return dispatch(frame);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object r = dispatch(frame);
        // P3-M9: use Boxing.toLong for correct zero-extension of unsigned
        // kinds (uint32(0xFFFFFFFF) -> 4294967295L, not -1L). A bare
        // ((Number) r).longValue() sign-extends Integer-held uint32 values.
        return r == null ? 0L : Boxing.toLong(r, kind());
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) dispatch(frame)).intValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        Object r = dispatch(frame);
        return r == null ? 0d : ((Number) r).doubleValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return (Boolean) dispatch(frame);
    }

    @Override
    public String toString() {
        return "IfaceDispatchNode{" + dispatcher.methodName() + ", iface=" + dispatcher.ifaceId()
                + ", pic=" + dispatcher.picCount() + "/3"
                + ", megamorphic=" + dispatcher.megamorphic()
                + ", megamorphicTransitions=" + IfaceDispatcher.megamorphicTransitions() + "}";
    }
}
