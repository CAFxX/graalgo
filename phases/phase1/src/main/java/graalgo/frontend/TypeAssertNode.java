package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Type assertion {@code x.(T)} (M4, WS E).
 *
 * <p>Single-valued form: a failed assertion (or a nil interface) throws
 * {@link Panic} (M5, WS F): gc's {@code interface conversion} panic. The {@code lenient} form (used for the
 * {@code v, ok := x.(T)} decomposition) yields the zero value instead;
 * {@link TypeAssertOkNode} is its {@code ok} half. A successful assertion
 * unwraps the dynamic value (struct values deep-copy on unwrap); a typed
 * nil unwraps to the nil reference. Asserting to the empty interface
 * always succeeds; asserting to a non-empty interface is a factory error
 * (needs method tables, WS D).
 */
public final class TypeAssertNode extends GoExprNode {
    @Child private GoExprNode iface;
    private final GoTypeDescriptor asserted;
    private final boolean lenient;
    private final GoStructDescriptor structDesc; // for struct-kind zero values
    private final String ifaceName; // Go-syntax static interface type (e.g. "interface {}")
    private final String assertedName; // Go-syntax asserted type (e.g. "int")
    /**
     * P3-M3 (WS D): when asserting to a non-empty interface, the asserted
     * interface's method implkeys (method name + structural signature —
     * see export.go {@code implKeyOf}) and the per-document dynamic-type
     * method implkey sets (descriptor name → implkeys). Both null for
     * non-interface targets. The runtime implements check is a subset
     * test on implkeys, so a same-name/wrong-signature method does NOT
     * satisfy the assertion (gc parity; R1 P0-1).
     */
    private final List<String> assertedIfaceMethods;
    private final Map<String, Set<String>> dynamicMethodSets;

    public TypeAssertNode(GoExprNode iface, GoKind kind, GoTypeDescriptor asserted,
                          boolean lenient, GoStructDescriptor structDesc,
                          String ifaceName, String assertedName,
                          List<String> assertedIfaceMethods,
                          Map<String, Set<String>> dynamicMethodSets) {
        super(kind);
        this.iface = iface;
        this.asserted = asserted;
        this.lenient = lenient;
        this.structDesc = structDesc;
        this.ifaceName = ifaceName;
        this.assertedName = assertedName;
        this.assertedIfaceMethods = assertedIfaceMethods;
        this.dynamicMethodSets = dynamicMethodSets;
    }

    public GoExprNode iface() {
        return iface;
    }

    /** The assertion predicate shared with {@link TypeAssertOkNode}. */

    /**
     * P3-M3 (WS D): assertion to a non-empty interface holds when the
     * dynamic type's method implkey set covers the asserted interface's
     * implkeys (the runtime half of the implements check; go/types verified
     * the static side where one exists — assertions from the empty
     * interface have no static side, hence the runtime check). Implkeys
     * (not bare names) make the check signature-aware: a method with the
     * right name but the wrong signature does not satisfy the assertion.
     *
     * <p>P3-M3 remediation (R1 P1-2): when the dynamic type is a generic
     * instantiation whose method table was skipped (the
     * {@link NodeFactory#TEMPLATE_METHODS_SKIPPED} sentinel), the check
     * cannot be decided — fail fast with the named P3-M3 scope error
     * instead of reporting "not implemented" (a bare {@code false} would
     * be a silent divergence vs gc, which decides by the real method set).
     */
    static boolean holds(GoInterface gi, GoTypeDescriptor asserted,
                         List<String> assertedIfaceMethods,
                         Map<String, Set<String>> dynamicMethodSets) {
        if (asserted.isEmptyInterface()) {
            return !gi.isNil();
        }
        if (gi.isNil()) {
            return false;
        }
        if (assertedIfaceMethods != null && dynamicMethodSets != null) {
            Set<String> dynMethods = dynamicMethodSets.getOrDefault(
                    ((GoTypeDescriptor) gi.descriptor()).name(), Set.of());
            if (dynMethods.contains(NodeFactory.TEMPLATE_METHODS_SKIPPED)) {
                throw new FrontendException("cannot assert generic instantiation "
                        + ((GoTypeDescriptor) gi.descriptor()).name() + " to interface "
                        + asserted.name() + ": method tables for instantiated methods"
                        + " are not supported (P3-M3 scope)");
            }
            return dynMethods.containsAll(assertedIfaceMethods);
        }
        return gi.descriptor() == asserted;
    }

    private Object read(VirtualFrame frame) {
        // P3-M7: a nil interface may flow as Java null — narrow to NIL so
        // the nil-interface paths below apply instead of NPEing.
        GoInterface gi = GoInterface.narrow(iface.executeObject(frame));
        if (!holds(gi, asserted, assertedIfaceMethods, dynamicMethodSets)) {
            if (lenient) {
                return Boxing.zero(kind(), structDesc);
            }
            // gc: "interface conversion: <iface> is <dynamic>, not <asserted>".
            // The dynamic name comes from the descriptor; the int/int64 and
            // uint/uint64/uintptr conflation is an M4 kind-system limitation
            // (GoKind has a single INT64/UINT64 slot; not part of GG-0045).
            String dynamicName = gi.isNil() ? "nil"
                    : ((GoTypeDescriptor) gi.descriptor()).name();
            Panic.throwPanic(GoErrors.interfaceConversion(ifaceName, dynamicName, assertedName), null, null, this);
        }
        if (asserted.isEmptyInterface()) {
            return gi;
        }
        if (assertedIfaceMethods != null) {
            // P3-M3 (WS D): asserting to a non-empty interface yields an
            // interface value — the (descriptor, data) pair passes through
            // unchanged; only the static type changes.
            return gi;
        }
        Object data = gi.data();
        if (data == null) {
            return null; // typed nil
        }
        return Boxing.copyForAssign(kind(), data);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return ((Number) read(frame)).longValue();
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) read(frame)).intValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Float) read(frame)).floatValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Double) read(frame)).doubleValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return ((Boolean) read(frame)).booleanValue();
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return read(frame);
    }
}
