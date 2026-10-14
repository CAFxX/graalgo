package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * The {@code ok} projection of a comma-ok type assertion
 * ({@code v, ok := x.(T)}, M4 WS E): whether the assertion holds.
 */
public final class TypeAssertOkNode extends GoExprNode {
    @Child private GoExprNode iface;
    private final GoTypeDescriptor asserted;

    public TypeAssertOkNode(GoExprNode iface, GoTypeDescriptor asserted) {
        super(GoKind.BOOL);
        this.iface = iface;
        this.asserted = asserted;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return TypeAssertNode.holds((GoInterface) iface.executeObject(frame), asserted);
    }
}
