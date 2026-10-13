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
        // P3-M8: long/int-kind children (unsafe.Pointer handles) test the
        // zero carrier, not a null reference.
        boolean isNull = switch (child.kind().slotKind()) {
            case Long -> child.executeLong(frame) == 0L;
            case Int -> child.executeInt(frame) == 0;
            default -> {
                Object v = child.executeObject(frame);
                // P3-M8b: a *T nil is GoPointer(null referent), not Java null.
                // P3-M9: ask the pointer itself — a cell pointer (&x) is
                // never nil even when x currently holds a nil value.
                if (v instanceof graalgo.values.GoPointer gp) {
                    yield gp.isNil();
                }
                yield v == null;
            }
        };
        return negated != isNull;
    }
}
