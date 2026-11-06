package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;
import graalgo.values.GoTuple;

/**
 * Packs several evaluated values into one {@link GoTuple} (V12-P1-2).
 *
 * <p>Built for a multi-value {@code return} ({@code return a, b}): the
 * children are evaluated left-to-right, each boxed to its canonical form
 * (see {@link Boxing#box}), and packed. A single-result return never uses
 * this node, and a {@code return f()} passes the call's own tuple through
 * untouched — only a return with two or more result expressions packs.
 */
public final class TupleNode extends GoExprNode {
    @Children private final GoExprNode[] values;

    public TupleNode(GoExprNode[] values) {
        super(GoKind.INTERFACE);
        this.values = values;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object[] boxed = new Object[values.length];
        for (int i = 0; i < values.length; i++) {
            boxed[i] = Boxing.box(values[i].kind(), values[i], frame);
        }
        return new GoTuple(boxed);
    }
}
