package graalgo.frontend;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.runtime.DeferChain;
import graalgo.runtime.Goexit;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.PanicException;
import graalgo.safepoint.Safepoint;

/**
 * Root node for a compiled {@code func main}.
 *
 * <p>Installs the Phase-1 {@link GoroutineState} (single main goroutine) and
 * records the defer watermark on entry. On exit the {@link DeferChain}
 * drains to the watermark:
 * <ul>
 *   <li>normal return / fall off end → run defers, return the value (the
 *       M2 test-observability channel; real programs observe via println);
 *   <li>panic → run defers via unwind; an unrecovered panic propagates to
 *       the launcher (exit 2, gc-comparable output); a recovered panic
 *       resumes after the chain;
 *   <li>goexit → run defers, rethrow (the launcher reports gc's fatal
 *       deadlock for main, exit 2).
 * </ul>
 * The D8 {@code Safepoint.poll()} seam fires at this function entry (no-op
 * in Phase 1).
 */
public final class GoRootNode extends RootNode {
    @Child private GoStmtNode body;
    private SourceSection sourceSection;

    public GoRootNode(FrameDescriptor descriptor, GoStmtNode body) {
        super((TruffleLanguage<?>) null, descriptor);
        this.body = body;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Safepoint.poll(); // D8 seam: no-op in Phase 1; Phase 2 gives it semantics.
        GoroutineState state = new GoroutineState();
        GoroutineState prev = GoroutineState.install(state);
        DeferChain chain = state.deferChain();
        int mark = chain.depth();
        GoStackFrameHead head = new GoStackFrameHead(state.pushGoStack("main", getSourceSection()));
        try {
            try {
                body.executeVoid(frame);
            } catch (GoReturn r) {
                chain.unwindReturning(mark);
                return r.value;
            }
            chain.unwindReturning(mark);
            return 0L;
        } catch (PanicException p) {
            PanicException rest = chain.unwindPanicking(mark, p);
            if (rest != null) {
                throw rest;
            }
            // Recovered at main level: resume after the chain.
            return 0L;
        } catch (Goexit g) {
            chain.unwindGoexiting(mark);
            throw g;
        } finally {
            state.popGoStack(head.frame);
            GoroutineState.restore(prev);
        }
    }

    private static final class GoStackFrameHead {
        final graalgo.runtime.GoStackFrame frame;
        GoStackFrameHead(graalgo.runtime.GoStackFrame frame) {
            this.frame = frame;
        }
    }

    /** Test/introspection aid: lets a test walk the built tree. */
    public GoStmtNode getBody() {
        return body;
    }

    /** Sets this node's source section (Truffle 25.x removed Node.setSourceSection). */
    public void setSourceSection(SourceSection section) {
        this.sourceSection = section;
    }

    @Override
    public SourceSection getSourceSection() {
        return sourceSection;
    }
}
