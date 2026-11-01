package graalgo.iface;

import java.util.List;
import java.util.Map;
import java.util.function.Consumer;

import com.oracle.truffle.api.nodes.Node;

import graalgo.frontend.FrontendException;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoErrors;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoTypeDescriptor;

/**
 * Shared interface method dispatch (P1-9, review-2026-10-31-22): the
 * implementation behind both the {@code "ifacemethod"} call node
 * ({@link IfaceDispatchNode}) and the {@code "ifacemethodvalue"} value
 * node (whose call-time root, {@link IfaceMethodValueRootNode}, invokes
 * this with the bound interface value). One implementation, two entry
 * points: a direct call evaluates its receiver and argument nodes and
 * dispatches immediately; a method value binds the evaluated receiver
 * and dispatches on each later call through the same itab machinery,
 * so a value formed on one dynamic type keeps dispatching correctly
 * even if the variable holding it is reassigned (the bound interface
 * value, not the variable, is what is bound — gc semantics).
 *
 * <p>The itab is built once per (descriptor name, interface id) as an
 * immutable {@link IfaceItab} (D6) and cached in {@link IfaceMethodTable}.
 * Dispatch polymorphic-caches on <em>itab identity</em>, limit 3; the 4th
 * distinct itab flips the site megamorphic with a {@link java.util.HashMap}
 * fallback keyed on itab identity (still correct, no deopt — itabs are
 * immutable per D6). Megamorphic transitions are counted in a static
 * atomic for observability, shared across all dispatchers (as the
 * counter was shared across all dispatch nodes before this factoring).
 *
 * <p>Invocation follows the {@code FuncValueCallNode} protocol:
 * {@code GoCalls.invoke(goFunc, prepend(data, argValues))} — the
 * receiver's {@code data} as the method's params[0], exactly like
 * {@code methodref} — including the {@code recoverAllowed} dance.
 */
public final class IfaceDispatcher {

    /** PIC limit (SPEC §3.3). */
    private static final int PIC_LIMIT = 3;

    private final String methodName;
    private final List<String> ifaceMethodNames;
    private final int ifaceId;

    /** One PIC slot: an itab identity and the resolved method target. Immutable. */
    private record PicSlot(IfaceItab itab, IfaceMethodTarget target) {}

    private volatile PicSlot pic0;
    private volatile PicSlot pic1;
    private volatile PicSlot pic2;
    /** Megamorphic fallback (identity-keyed: IfaceItab has no equals). Written under lock. */
    private volatile Map<IfaceItab, IfaceMethodTarget> megamorphic;
    /**
     * Process-wide count of megamorphic transitions (observability).
     * Atomic: the increment races across dispatch threads (R2 P2-2) —
     * a lost update would silently undercount transitions.
     */
    private static final java.util.concurrent.atomic.AtomicLong megamorphicTransitions =
            new java.util.concurrent.atomic.AtomicLong();

    public IfaceDispatcher(String methodName, List<String> ifaceMethodNames, int ifaceId) {
        this.methodName = methodName;
        this.ifaceMethodNames = List.copyOf(ifaceMethodNames);
        this.ifaceId = ifaceId;
    }

    /** The dispatched method name (probe site naming, diagnostics). */
    public String methodName() {
        return methodName;
    }

    /** The interface type-table id this dispatcher dispatches through. */
    public int ifaceId() {
        return ifaceId;
    }

    /** Process-wide megamorphic-transition count (observability). */
    public static long megamorphicTransitions() {
        return megamorphicTransitions.get();
    }

    /** Resets the transition counter (tests). */
    static void resetMegamorphicTransitions() {
        megamorphicTransitions.set(0);
    }

    /** Number of filled PIC slots (diagnostics, {@code toString}). */
    int picCount() {
        return (pic0 != null ? 1 : 0) + (pic1 != null ? 1 : 0) + (pic2 != null ? 1 : 0);
    }

    /** True once this site has gone megamorphic (diagnostics). */
    boolean megamorphic() {
        return megamorphic != null;
    }

    /**
     * Dispatches one interface method call.
     *
     * @param recvVal   the evaluated receiver: a {@link GoInterface}, or
     *                  Java null for a nil interface value (panics with
     *                  gc's nil-dereference panic, exactly like calling
     *                  a nil func value)
     * @param argVals   the already-boxed call arguments (receiver NOT
     *                  included; the method's params[0] is the
     *                  receiver's {@code data}, derived here)
     * @param panicNode the node reported as the panic/call site
     * @param probe     called with each itab consulted (the M9
     *                  re-measurement hook); callers pass their own
     *                  per-site probe, or {@code itab -> {}} to skip it
     * @return the call's single result: a D5 value, the pair struct for
     *         a multi-result method (the itab target is its
     *         pair-returning form), or null for a void method
     */
    public Object dispatch(Object recvVal, Object[] argVals, Node panicNode,
            Consumer<IfaceItab> probe) {
        GoInterface gi = requireReceiver(recvVal, panicNode);
        Object data = gi.data(); // null for typed-nil: passed through as params[0]
        String descName = ((GoTypeDescriptor) gi.descriptor()).name();
        IfaceItab itab = IfaceMethodTable.itabFor(descName, ifaceId, ifaceMethodNames);
        probe.accept(itab);
        IfaceMethodTarget target = lookupPic(itab);
        // R2 P2-7: an unsupported promotion (pointer receiver via a
        // value-embedded field — wire "index" null) fails fast here, only
        // when the method is actually dispatched. The itab build carries
        // the unsupported target precisely so programs dispatching only
        // supported methods through the interface keep working.
        if (!target.supported()) {
            throw new FrontendException("interface dispatch: method \"" + methodName
                    + "\" promoted via value-embedded field is not supported (P3-M3 scope)");
        }
        // P3-M9 (x509/TLS lane): method promoted via an embedded interface
        // field (e.g. (*crypto/tls.listener).Addr from embedded
        // net.Listener). The entry carries no GoFunc: read the embedding
        // path to the interface field value and dispatch the method on it,
        // exactly like the direct l.Listener.Addr() call. Embedding can
        // chain through several interfaces, so loop until a concrete
        // target is reached.
        GoInterface curGi = gi;
        while (target.isEmbeddedRedispatch()) {
            Object fieldVal = graalgo.frontend.SelectorNode.readFieldPath(data, target.recvPath(),
                    false, panicNode);
            curGi = requireReceiver(fieldVal, panicNode);
            data = curGi.data();
            String innerDesc = ((GoTypeDescriptor) curGi.descriptor()).name();
            IfaceItab innerItab = IfaceMethodTable.itabFor(innerDesc, target.embeddedIfaceId(),
                    target.embeddedIfaceMethods());
            probe.accept(innerItab);
            IfaceMethodTarget inner = innerItab.methods().get(methodName);
            if (inner == null) {
                // Unreachable: go/types verified the embedded interface
                // declares the method, so the field value's itab has it.
                throw new FrontendException("interface dispatch: method \"" + methodName
                        + "\" missing from itab for \"" + innerItab.descriptorName()
                        + "\" (internal error)");
            }
            if (!inner.supported()) {
                throw new FrontendException("interface dispatch: method \"" + methodName
                        + "\" promoted via value-embedded field is not supported (P3-M3 scope)");
            }
            target = inner;
        }
        Object[] callArgs = new Object[argVals.length + 1];
        // P3-M3: promoted methods receive the embedded value — walk the
        // exporter's "index" path from the dynamic value. A value-receiver
        // method promoted via a pointer-embedded field gets the trailing
        // dereference (gc's implicit (*p)).
        callArgs[0] = target.isDirect()
                ? data
                : graalgo.frontend.SelectorNode.readFieldPath(data, target.recvPath(),
                        !target.pointerReceiver(), panicNode);
        // P3-M9 (ecdsa/hash root fix): a desugared helper installed as a
        // method-table target may declare its receiver parameter as an
        // INTERFACE (e.g. hash.__hashWrite(h hash.Hash, p []byte)) instead
        // of the concrete dynamic type. Passing the raw dynamic value
        // makes the callee root fabricate a descriptor-less box
        // (GoFuncRootNode.wrapInterface), which loses the dynamic type's
        // method set — interface assertions inside the helper then fail
        // (the "itab loses the method set" defect the hash/* packages
        // worked around with concrete-receiver helpers). Go semantics:
        // an interface-typed parameter receives the interface value
        // itself, dynamic descriptor intact. Concrete-receiver targets
        // keep the raw value, exactly as before.
        if (target.isDirect() && receiverParamIsInterface(target)) {
            callArgs[0] = curGi;
        }
        // P3-M9: pointer-receiver method promoted via a value-embedded
        // struct field — take the address of the promoted field value
        // (gc's implicit &). Struct fields are read by reference, so the
        // pointer targets the live field object and mutations through it
        // are visible to the outer struct, exactly like gc.
        if (target.addrOf() && !(callArgs[0] instanceof GoPointer)) {
            callArgs[0] = new GoPointer(callArgs[0]);
        }
        System.arraycopy(argVals, 0, callArgs, 1, argVals.length);
        // FuncValueCallNode's recoverAllowed dance: recover() is only
        // non-nil when called directly by a deferred function — clear the
        // flag for this nested call, restore afterwards.
        GoroutineState state = GoroutineState.current();
        boolean prev = state != null && state.isRecoverAllowed();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        try {
            return GoCalls.invoke(target.func(), callArgs);
        } finally {
            if (state != null) {
                state.setRecoverAllowed(prev);
            }
        }
    }

    /**
     * The receiver must be a non-nil interface value. Nil (Java null from
     * a nil interface zero value, or a descriptor-less {@link GoInterface})
     * panics exactly like calling a nil func value.
     */
    static GoInterface requireReceiver(Object r, Node panicNode) {
        if (r instanceof GoInterface gi) {
            if (!gi.isNil()) {
                return gi;
            }
            Panic.throwPanic(GoErrors.nilDereference(), null, null, panicNode);
            throw new AssertionError("unreachable: Panic.throwPanic returned");
        }
        if (r == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, panicNode);
            throw new AssertionError("unreachable: Panic.throwPanic returned");
        }
        throw new FrontendException("ifacemethod: receiver evaluated to "
                + r.getClass().getName() + ", not a GoInterface (internal error)");
    }

    /**
     * True when the target function's first parameter (the receiver slot)
     * is interface-kinded — the signature of a desugared helper installed
     * in a method table (its "receiver" is a plain interface parameter).
     * Read off the callee's {@link graalgo.frontend.GoFuncRootNode}
     * parameter kinds; non-Go roots (none today) answer false.
     */
    private static boolean receiverParamIsInterface(IfaceMethodTarget target) {
        if (target.func().target().getRootNode()
                instanceof graalgo.frontend.GoFuncRootNode root) {
            GoKind[] kinds = root.paramKinds();
            return kinds.length > 0 && kinds[0] == GoKind.INTERFACE;
        }
        return false;
    }

    private IfaceMethodTarget lookupPic(IfaceItab itab) {
        PicSlot s0 = pic0;
        if (s0 != null && s0.itab == itab) {
            return s0.target;
        }
        PicSlot s1 = pic1;
        if (s1 != null && s1.itab == itab) {
            return s1.target;
        }
        PicSlot s2 = pic2;
        if (s2 != null && s2.itab == itab) {
            return s2.target;
        }
        return lookupPicMiss(itab);
    }

    private synchronized IfaceMethodTarget lookupPicMiss(IfaceItab itab) {
        // Recheck under the lock: another thread may have filled the slot.
        PicSlot s0 = pic0;
        if (s0 != null && s0.itab == itab) {
            return s0.target;
        }
        PicSlot s1 = pic1;
        if (s1 != null && s1.itab == itab) {
            return s1.target;
        }
        PicSlot s2 = pic2;
        if (s2 != null && s2.itab == itab) {
            return s2.target;
        }
        IfaceMethodTarget t = itab.methods().get(methodName);
        if (t == null) {
            // Unreachable: itabFor already failed fast on a missing method.
            throw new FrontendException("interface dispatch: method \"" + methodName
                    + "\" missing from itab for \"" + itab.descriptorName() + "\" (internal error)");
        }
        if (pic0 == null) {
            pic0 = new PicSlot(itab, t);
            return t;
        }
        if (pic1 == null) {
            pic1 = new PicSlot(itab, t);
            return t;
        }
        if (pic2 == null) {
            pic2 = new PicSlot(itab, t);
            return t;
        }
        Map<IfaceItab, IfaceMethodTarget> mm = megamorphic;
        if (mm == null) {
            mm = new java.util.HashMap<>();
            megamorphic = mm;
            megamorphicTransitions.incrementAndGet();
        }
        IfaceMethodTarget mt = mm.get(itab);
        if (mt == null) {
            mm.put(itab, t);
            return t;
        }
        return mt;
    }
}
