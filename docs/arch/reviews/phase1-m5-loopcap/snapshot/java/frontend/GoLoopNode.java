package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.LoopNode;
import com.oracle.truffle.api.nodes.RepeatingNode;

/**
 * Truffle {@link LoopNode} wrapper for Go loops (D8).
 * <p>
 * Every Go loop is a {@code LoopNode} driving a {@link RepeatingNode};
 * the back-edge of each repeating node calls {@link graalgo.safepoint.Safepoint#poll()}
 * (the frozen D8 seam, mirroring the function-entry seam from M3).
 * <p>
 * Truffle 25.x's {@code LoopNode.execute} must be overridden by the
 * subclass (it throws {@link AbstractMethodError} otherwise); the loop is
 * driven here, with {@link #getRepeatingNode()} for runtime introspection.
 */
public final class GoLoopNode extends LoopNode {
    @Child private RepeatingNode repeating;

    public GoLoopNode(RepeatingNode repeating) {
        this.repeating = repeating;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        while (repeating.executeRepeating(frame)) {
            // The repeating node returns false to end the loop.
        }
        return null;
    }

    @Override
    public RepeatingNode getRepeatingNode() {
        return repeating;
    }
}
