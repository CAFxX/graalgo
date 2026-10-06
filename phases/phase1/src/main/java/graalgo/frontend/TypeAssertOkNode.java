package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * The {@code ok} projection of a comma-ok type assertion
 * ({@code v, ok := x.(T)}, M4 WS E): whether the assertion holds.
 */
public final class TypeAssertOkNode extends GoExprNode {
    @Child private GoExprNode iface;
    private final GoTypeDescriptor asserted;
    /** P3-M3 (WS D): see {@link TypeAssertNode}; null for non-interface targets. */
    private final List<String> assertedIfaceMethods;
    private final Map<String, Set<String>> dynamicMethodSets;

    public TypeAssertOkNode(GoExprNode iface, GoTypeDescriptor asserted,
                            List<String> assertedIfaceMethods,
                            Map<String, Set<String>> dynamicMethodSets) {
        super(GoKind.BOOL);
        this.iface = iface;
        this.asserted = asserted;
        this.assertedIfaceMethods = assertedIfaceMethods;
        this.dynamicMethodSets = dynamicMethodSets;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return TypeAssertNode.holds((GoInterface) iface.executeObject(frame), asserted,
                assertedIfaceMethods, dynamicMethodSets);
    }
}
