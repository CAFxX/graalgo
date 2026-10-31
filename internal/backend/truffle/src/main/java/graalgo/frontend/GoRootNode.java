package graalgo.frontend;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotTypeException;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.runtime.DeferChain;
import graalgo.runtime.DeferredCall;
import graalgo.runtime.Goexit;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.PanicException;
import graalgo.safepoint.Safepoint;
import java.util.List;

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
    /**
     * PKG-039: frame slot holding main's range-over-func defer stash, or
     * -1 when main contains no range-over-func (see
     * {@link RangeFuncNode}).
     */
    private final int rangeStashSlot;

    public GoRootNode(FrameDescriptor descriptor, GoStmtNode body) {
        this(descriptor, body, -1);
    }

    /**
     * @param rangeStashSlot PKG-039: frame slot holding main's
     *                       range-over-func defer stash, or -1 when main
     *                       contains no range-over-func.
     */
    public GoRootNode(FrameDescriptor descriptor, GoStmtNode body, int rangeStashSlot) {
        super((TruffleLanguage<?>) null, descriptor);
        this.body = body;
        this.rangeStashSlot = rangeStashSlot;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        // D8 seam: Phase-2 quantum check. The Phase-1 Safepoint copy was
        // a no-op; the launchers' overlay check (R-02) guarantees the
        // Phase-2 Safepoint loads, so this call is never silently empty.
        Safepoint.poll();
        GoroutineState state = new GoroutineState();
        GoroutineState prev = GoroutineState.install(state);
        DeferChain chain = state.deferChain();
        int mark = chain.depth();
        GoStackFrameHead head = new GoStackFrameHead(state.pushGoStack("main", getSourceSection()));
        try {
            try {
                body.executeVoid(frame);
            } catch (GoReturn r) {
                adoptRangeDefers(chain, frame, mark);
                chain.unwindReturning(mark);
                return r.value;
            }
            adoptRangeDefers(chain, frame, mark);
            chain.unwindReturning(mark);
            return 0L;
        } catch (PanicException p) {
            adoptRangeDefers(chain, frame, mark);
            PanicException rest = chain.unwindPanicking(mark, p);
            if (rest != null) {
                throw rest;
            }
            // Recovered at main level: resume after the chain.
            return 0L;
        } catch (Goexit g) {
            adoptRangeDefers(chain, frame, mark);
            chain.unwindGoexiting(mark);
            throw g;
        } finally {
            state.popGoStack(head.frame);
            GoroutineState.restore(prev);
        }
    }

    /**
     * PKG-039: merges main's range-over-func defer stash back into the
     * chain before unwinding (gc's {@code deferrangefunc}). No-op when
     * main contains no range-over-func or no body defer was stashed.
     */
    private void adoptRangeDefers(DeferChain chain, VirtualFrame frame, int mark) {
        if (rangeStashSlot < 0) {
            return;
        }
        Object cur;
        try {
            cur = frame.getObject(rangeStashSlot);
        } catch (FrameSlotTypeException e) {
            return; // stash slot never initialized: no range executed
        }
        if (cur instanceof List<?> stash) {
            @SuppressWarnings("unchecked")
            List<DeferredCall> calls = (List<DeferredCall>) stash;
            chain.adoptRangeDefers(mark, calls);
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
