package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.LoopNode;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;

/**
 * Go loop driver (D8): wraps the runtime's {@link LoopNode} around a
 * {@link RepeatingNode}; the back-edge of each repeating node calls
 * {@link graalgo.safepoint.Safepoint#poll()} (the frozen D8 seam,
 * mirroring the function-entry seam from M3).
 *
 * <p>The loop node comes from {@link Truffle#getRuntime()}{@code
 * .createLoopNode}: on the optimizing runtime that is an OSR loop node,
 * which counts back-edges and compiles a hot loop on its own (on-stack
 * replacement) even when its enclosing function is entered only once —
 * {@code main}'s loops, a goroutine body's {@code for {}}. A hand-written
 * {@code LoopNode} subclass (the pre-T0.B shape) never OSR-compiles, so
 * long-running loops stayed in the interpreter forever. On the default
 * (interpreter-only) runtime the same call returns a plain loop.
 */
public final class GoLoopNode extends Node {
    @Child private LoopNode loop;
    private final RepeatingNode repeating;

    public GoLoopNode(RepeatingNode repeating) {
        this.repeating = repeating;
        this.loop = Truffle.getRuntime().createLoopNode(repeating);
    }

    /** Runs the loop to completion (the repeating node returns false to end it). */
    public Object execute(VirtualFrame frame) {
        return loop.execute(frame);
    }

    /** The loop body driver, for label wiring and introspection. */
    public RepeatingNode getRepeatingNode() {
        return repeating;
    }
}
