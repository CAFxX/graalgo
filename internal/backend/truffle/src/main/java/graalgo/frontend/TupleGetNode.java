package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;
import graalgo.values.GoTuple;

/**
 * Reads one element of a {@link GoTuple} (V12-P1-2).
 *
 * <p>Built for tuple destructuring ({@code a, b = f()},
 * {@code var a, b = f()}): the child evaluates to the tuple (usually a
 * frame-slot read of the temporary the factory stored the call result
 * in), and this node yields element {@code index}. The element kind is
 * the result's static kind, so the typed execute methods unbox the
 * canonically-boxed element exactly like a variable read of that kind.
 *
 * <p>Misuse fails loudly, never silently: a null tuple (a multi-value
 * function that fell off the end without the root synthesizing zeros — a
 * factory/root contract violation), a non-tuple value, or an index past
 * the tuple's arity all throw {@link FrontendException}.
 */
public final class TupleGetNode extends GoExprNode {
    @Child private GoExprNode tuple;
    private final int index;

    public TupleGetNode(GoExprNode tuple, int index, GoKind elementKind) {
        super(elementKind);
        this.tuple = tuple;
        this.index = index;
    }

    private Object element(VirtualFrame frame) {
        Object v = tuple.executeObject(frame);
        if (v == null) {
            throw new FrontendException(
                    "tuple element " + index + ": multi-value call returned no tuple "
                    + "(callee fell off the end without a value)");
        }
        if (!(v instanceof GoTuple t)) {
            throw new FrontendException(
                    "tuple element " + index + ": expected GoTuple, got "
                    + v.getClass().getSimpleName());
        }
        if (index < 0 || index >= t.size()) {
            throw new FrontendException(
                    "tuple element " + index + ": tuple has " + t.size() + " values");
        }
        return t.get(index);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return element(frame);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return Boxing.toLong(element(frame), kind());
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) element(frame)).intValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Number) element(frame)).floatValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Number) element(frame)).doubleValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return (Boolean) element(frame);
    }
}
