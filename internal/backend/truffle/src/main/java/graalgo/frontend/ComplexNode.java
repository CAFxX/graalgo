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
        // real/imag of a complex64 — but the argument's representation is
        // not determined by this execute method: an untyped complex
        // constant (e.g. the folded argument of real(3+4i)) is built as a
        // GoComplex64 while this node's kind is the call's
        // contextually-typed result (float32 for a float32 consumer),
        // so a float32 consumer dispatches here over a complex128 value
        // (R22-F-6). Dispatch on the argument's actual representation;
        // the narrowing cast is correctly rounded, matching gc.
        Object v = first.executeObject(frame);
        if (v instanceof GoComplex64 c64) {
            return (float) (op == Op.REAL ? c64.re() : c64.im());
        }
        GoComplex32 c32 = (GoComplex32) v;
        return op == Op.REAL ? c32.re() : c32.im();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        // real/imag of a complex128 — same representation caveat as
        // executeFloat (R22-F-6): a complex64 argument (e.g. a folded
        // complex(1, 2) constant consumed as float64) dispatches here.
        // Dispatch on the argument's actual representation; the widening
        // conversion is exact.
        Object v = first.executeObject(frame);
        if (v instanceof GoComplex32 c32) {
            return op == Op.REAL ? c32.re() : c32.im();
        }
        GoComplex64 c64 = (GoComplex64) v;
        return op == Op.REAL ? c64.re() : c64.im();
    }
}
