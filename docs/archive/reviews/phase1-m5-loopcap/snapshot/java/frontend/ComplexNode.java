package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code complex}, {@code real}, and {@code imag} builtins (WS G;
 * verifies GG-0020).
 *
 * <ul>
 *   <li>{@code complex(re, im)}: {@code float32} parts give
 *       {@code complex64}, {@code float64} parts give {@code complex128}.</li>
 *   <li>{@code real(c)} / {@code imag(c)}: {@code complex64} gives
 *       {@code float32}, {@code complex128} gives {@code float64}.</li>
 * </ul>
 */
public final class ComplexNode extends GoExprNode {
    public enum Op { COMPLEX, REAL, IMAG }

    @Child private GoExprNode first;
    @Child private GoExprNode second; // null for REAL/IMAG
    private final Op op;

    public ComplexNode(Op op, GoKind kind, GoExprNode first, GoExprNode second) {
        super(kind);
        this.op = op;
        this.first = first;
        this.second = second;
        if ((op == Op.REAL || op == Op.IMAG) && second != null) {
            throw new IllegalArgumentException("real/imag take one argument");
        }
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (op != Op.COMPLEX) {
            throw new UnsupportedOperationException("real/imag use typed executes");
        }
        if (kind() == GoKind.COMPLEX64) {
            return new GoComplex32(first.executeFloat(frame), second.executeFloat(frame));
        }
        return new GoComplex64(first.executeDouble(frame), second.executeDouble(frame));
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        // real/imag of a complex64.
        GoComplex32 c = (GoComplex32) first.executeObject(frame);
        return op == Op.REAL ? c.re() : c.im();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        // real/imag of a complex128.
        GoComplex64 c = (GoComplex64) first.executeObject(frame);
        return op == Op.REAL ? c.re() : c.im();
    }
}
