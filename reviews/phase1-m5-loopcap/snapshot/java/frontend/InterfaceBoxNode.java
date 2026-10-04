package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * Concrete-to-interface boxing (M4, WS E): {@code var i any = v}.
 *
 * <p>The dynamic type is the value's <em>named</em> type (a named type and
 * its underlying type are different dynamic types, gc-verified); an
 * untyped constant takes its default type ({@code int}, {@code float64},
 * {@code string}, {@code bool}). The data is the value in its D5 carrier,
 * through the value-copy rule (struct values deep-copy on box, so later
 * mutation of the variable is not observed through the interface).
 */
public final class InterfaceBoxNode extends GoExprNode {
    @Child private GoExprNode child;
    private final GoKind dataKind;
    private final GoTypeDescriptor descriptor;

    /**
     * @param child the concrete value; its {@link GoExprNode#typeId()} is
     *              the static type id (for named-type identity)
     * @param dataKind the kind to read the child at (default kind for
     *                 untyped constants)
     * @param descriptor the canonical dynamic-type descriptor
     */
    public InterfaceBoxNode(GoExprNode child, GoKind dataKind, GoTypeDescriptor descriptor) {
        super(GoKind.INTERFACE);
        this.child = child;
        this.dataKind = dataKind;
        this.descriptor = descriptor;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object data = Boxing.copyForAssign(dataKind, Boxing.box(dataKind, child, frame));
        return new GoInterface(descriptor, data, null);
    }
}
