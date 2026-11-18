package graalgo.language;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.frontend.NodeFactory;

/**
 * The call target {@link GoLanguage#parse} returns: executing it runs the
 * program in the current {@link GoContext} and yields the exit status
 * ({@code int}). Internal: the program's own functions are the root nodes
 * a tool should show.
 */
final class GoProgramRootNode extends RootNode {
    private final NodeFactory.Program program;

    GoProgramRootNode(GoLanguage language, NodeFactory.Program program) {
        super(language);
        this.program = program;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        return GoContext.get(this).run(program);
    }

    @Override
    public boolean isInternal() {
        return true;
    }

    @Override
    public String getName() {
        return "<go program>";
    }
}
