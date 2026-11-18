package graalgo.frontend;

/**
 * {@code fallthrough} as a control-flow signal (M4, WS E). Thrown by the
 * final statement of a switch case clause; caught by the enclosing
 * {@link SwitchNode}, which executes the next clause's body without
 * re-testing its case expressions. go/types guarantees it only appears as
 * the final statement of a non-final clause.
 */
public final class FallthroughSignal extends com.oracle.truffle.api.nodes.ControlFlowException {
    public FallthroughSignal() {

    }
}
