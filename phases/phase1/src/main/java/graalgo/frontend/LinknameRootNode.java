package graalgo.frontend;

import java.util.Arrays;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;

import graalgo.runtime.Linknames;
import graalgo.safepoint.Safepoint;

/**
 * Root node for a {@code //go:linkname}-bound function (P3-M4, WS C,
 * ADR-0004).
 *
 * <p>Calling convention (same as {@link GoFuncRootNode}): {@code args[0]}
 * is the {@link graalgo.values.GoFunc}, {@code args[1..]} are the call
 * arguments in D5 boxed form. The call dispatches by symbol name through
 * {@link Linknames#dispatch}; no Go stack frame is pushed (gc's assembly
 * intrinsics are likewise invisible in traces) and no defer chain runs
 * (a linkname target is not a Go function). {@link Safepoint#poll()}
 * runs on entry, matching {@code GoFuncRootNode}.
 *
 * <p>verifies: GG-0020
 */
public final class LinknameRootNode extends RootNode {
    private final String symbol;
    private final String funcName;
    private SourceSection sourceSection;

    public LinknameRootNode(String symbol, String funcName) {
        super((TruffleLanguage<?>) null, FrameDescriptor.newBuilder().build());
        this.symbol = symbol;
        this.funcName = funcName;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Safepoint.poll();
        Object[] args = frame.getArguments();
        Object[] callArgs = Arrays.copyOfRange(args, 1, args.length);
        return Linknames.dispatch(symbol, callArgs);
    }

    @Override
    public String getName() {
        return funcName;
    }

    public void setSourceSection(SourceSection section) {
        this.sourceSection = section;
    }

    @Override
    public SourceSection getSourceSection() {
        return sourceSection;
    }
}
