package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.BuiltinPrint;
import graalgo.values.GoKind;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code print} and {@code println} builtins (WS G; verifies GG-0020).
 *
 * <p>Both write to <b>stderr</b> (gc parity — see {@link BuiltinPrint}).
 * They are statement-only: {@link #executeLong} is implemented for
 * {@link ExprStmtNode}. {@code println} separates operands with spaces and
 * appends a newline; {@code print} separates with spaces only between
 * non-string operands (gc's walkPrint rules, in {@link BuiltinPrint}).
 */
public final class PrintNode extends GoExprNode {
    @Children private GoExprNode[] args;
    private final boolean newline;

    public PrintNode(boolean newline, GoExprNode[] args) {
        // No result; INTERFACE is a placeholder kind (only executeLong is
        // ever called, via ExprStmtNode).
        super(GoKind.INTERFACE);
        this.newline = newline;
        this.args = args;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        BuiltinPrint.Arg[] bargs = new BuiltinPrint.Arg[args.length];
        for (int i = 0; i < args.length; i++) {
            bargs[i] = new BuiltinPrint.Arg(args[i].kind(), readArg(args[i], frame));
        }
        if (newline) {
            BuiltinPrint.println(bargs);
        } else {
            BuiltinPrint.print(bargs);
        }
        return 0L;
    }

    private static Object readArg(GoExprNode child, VirtualFrame frame) {
        return switch (child.kind().slotKind()) {
            case Int -> child.executeInt(frame);
            case Long -> child.executeLong(frame);
            case Float -> child.executeFloat(frame);
            case Double -> child.executeDouble(frame);
            case Boolean -> child.executeBoolean(frame);
            case Object -> child.executeObject(frame);
            default -> throw new FrontendException("unsupported kind: " + child.kind());
        };
    }
}
