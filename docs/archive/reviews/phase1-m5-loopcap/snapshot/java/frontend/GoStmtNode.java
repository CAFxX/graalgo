package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.SourceSection;

/**
 * Base class for statement nodes built by {@link NodeFactory}.
 *
 * <p>WS E extends this family with control-flow statements (if/for/switch);
 * the {@link NodeFactory#registerStmtKind} hook is where new statement kinds
 * plug in.
 *
 * <p>Source sections: stored locally, exposed via {@link #getSourceSection()}
 * (Truffle 25.x removed {@code Node.setSourceSection}).
 */
public abstract class GoStmtNode extends Node {
    private SourceSection sourceSection;

    public abstract void executeVoid(VirtualFrame frame);

    /** Sets this node's source section (replaces the removed Node API). */
    public final void setSourceSection(SourceSection section) {
        this.sourceSection = section;
    }

    @Override
    public SourceSection getSourceSection() {
        return sourceSection;
    }
}
