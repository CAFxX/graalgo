package graalgo.language;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.go.loader.Program;

/**
 * The call target {@link GoLanguage#parse} returns for a linked bundle: executing it runs the
 * program in the current {@link GoContext} and yields the exit status ({@code int}).
 */
final class GoBundleRootNode extends RootNode {
    private final Program program;

    GoBundleRootNode(GoLanguage language, Program program) {
        super(language);
        this.program = program;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        return GoContext.get(this).runBundle(program);
    }

    @Override
    public boolean isInternal() {
        return true;
    }

    @Override
    public String getName() {
        return "<go bundle>";
    }
}
