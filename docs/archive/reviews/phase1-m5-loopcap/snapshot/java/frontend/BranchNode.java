package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;

/**
 * {@code break}, {@code continue}, {@code goto}, and {@code fallthrough}
 * (M4, WS E). Each throws its control-flow signal; the target construct
 * (loop, switch, labeled statement, block) catches it. The factory
 * validates placement only defensively — go/types rejects misplaced
 * branches before the factory runs.
 */
public final class BranchNode extends GoStmtNode {
    public enum Op {
        BREAK, CONTINUE, GOTO, FALLTHROUGH;

        static Op fromWire(String op, String path) {
            return switch (op) {
                case "break" -> BREAK;
                case "continue" -> CONTINUE;
                case "goto" -> GOTO;
                case "fallthrough" -> FALLTHROUGH;
                default -> throw new FrontendException(path + ": unknown branch op \"" + op + "\" (WS E)");
            };
        }
    }

    private final Op op;
    private final String label;

    public BranchNode(Op op, String label) {
        this.op = op;
        this.label = label;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        switch (op) {
            case BREAK -> throw new BreakSignal(label);
            case CONTINUE -> throw new ContinueSignal(label);
            case GOTO -> throw new GotoSignal(label);
            case FALLTHROUGH -> throw new FallthroughSignal();
        }
    }
}
