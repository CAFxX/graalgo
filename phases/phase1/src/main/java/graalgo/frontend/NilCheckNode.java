package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;

/**
 * {@code v == nil} / {@code v != nil} for a nillable concrete kind (M7).
 *
 * <p>A nil comparison against a slice, map, pointer, function, channel, or
 * unsafe.Pointer value tests the Java null carrier directly — no interface
 * boxing. (The M5 interface-equality path in {@link BinaryOpNode} only
 * covers INTERFACE operands; the exporter types every nil literal as the
 * empty interface, so the factory must recognize the pattern.)
 */
public final class NilCheckNode extends GoExprNode {
    @Child private GoExprNode child;
    private final boolean negated; // true for !=

    public NilCheckNode(GoExprNode child, boolean negated) {
        super(GoKind.BOOL);
        this.child = child;
        this.negated = negated;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        boolean isNull = child.executeObject(frame) == null;
        return negated != isNull;
    }
}
