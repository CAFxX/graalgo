package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;

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

    public TypeAssertNode(GoExprNode iface, GoKind kind, GoTypeDescriptor asserted,
                          boolean lenient, GoStructDescriptor structDesc,
                          String ifaceName, String assertedName) {
        super(kind);
        this.iface = iface;
        this.asserted = asserted;
        this.lenient = lenient;
        this.structDesc = structDesc;
        this.ifaceName = ifaceName;
        this.assertedName = assertedName;
    }

    public GoExprNode iface() {
        return iface;
    }

    /** The assertion predicate shared with {@link TypeAssertOkNode}. */
    static boolean holds(GoInterface gi, GoTypeDescriptor asserted) {
        if (asserted.isEmptyInterface()) {
            return !gi.isNil();
        }
        return !gi.isNil() && gi.descriptor() == asserted;
    }

    private Object read(VirtualFrame frame) {
        GoInterface gi = (GoInterface) iface.executeObject(frame);
        if (!holds(gi, asserted)) {
            if (lenient) {
                return Boxing.zero(kind(), structDesc);
            }
            // gc: "interface conversion: <iface> is <dynamic>, not <asserted>".
            // The dynamic name comes from the descriptor; the int/int64 and
            // uint/uint64/uintptr conflation is an M4 kind-system limitation
            // (GoKind has a single INT64/UINT64 slot; not part of GG-0045).
            String dynamicName = gi.isNil() ? "nil"
                    : ((GoTypeDescriptor) gi.descriptor()).name();
            Panic.throwPanic(GoErrors.interfaceConversion(ifaceName, dynamicName, assertedName));
        }
        if (asserted.isEmptyInterface()) {
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
        return ((Long) read(frame)).longValue();
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Integer) read(frame)).intValue();
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
