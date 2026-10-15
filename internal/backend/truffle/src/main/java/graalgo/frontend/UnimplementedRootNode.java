package graalgo.frontend;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;

import graalgo.runtime.Panic;
import graalgo.safepoint.Safepoint;

/**
 * Root node for a bodyless function without {@code //go:linkname}
 * (typically an assembly implementation in a .s file that graalgo does
 * not process).
 *
 * <p>Calling convention (same as {@link GoFuncRootNode}): {@code args[0]}
 * is the {@link graalgo.values.GoFunc}, {@code args[1..]} are the call
 * arguments in D5 boxed form. The call panics with a clear "unimplemented"
 * message. This is general: it applies uniformly to all bodyless functions,
 * preserving their type identity so references and value positions work,
 * while failing loudly (never silently) if the unimplemented function is
 * actually invoked.
 *
 * <p>This is not a bridge: it does not pretend to implement the function.
 * It explicitly marks the function as unimplemented, which is the honest
 * representation of the current state (equivalent to gc's link error for
 * a missing assembly implementation).
 */
public final class UnimplementedRootNode extends RootNode {
    private final String funcName;
    private final String pkgPath;
    private SourceSection sourceSection;

    public UnimplementedRootNode(String funcName, String pkgPath) {
        super((TruffleLanguage<?>) null, FrameDescriptor.newBuilder().build());
        this.funcName = funcName;
        this.pkgPath = pkgPath;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Safepoint.poll();
        String msg = "unimplemented: " + pkgPath + "." + funcName
                + " (assembly function not implemented in graalgo)";
        Panic.throwPanic(msg, null, null, this);
        throw new IllegalStateException("throwPanic returned");
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
