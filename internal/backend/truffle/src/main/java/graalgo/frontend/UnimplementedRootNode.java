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
 *
 * <p>Exception (fix-redmaster-r28, Fix 4): when a Java intrinsic (or a
 * bound Go function) is registered under {@code pkgpath.name}, the stub
 * dispatches to it instead of panicking — gc resolves bodyless
 * "implemented in package runtime" declarations to the runtime's
 * implementation, and a registered intrinsic is that implementation.
 * A symbol with no registered implementation still panics here.
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
        // fix-redmaster-r28 (Fix 4): an assembly function with a
        // registered implementation is not unimplemented — gc resolves
        // bodyless "implemented in package runtime" declarations (e.g.
        // reflect.unsafe_New) to the runtime's implementation, and a
        // Linknames intrinsic (or bound Go function) under
        // pkgpath.name is exactly that implementation. Dispatch to it
        // (arguments are the D5-boxed call arguments, same convention
        // as LinknameRootNode); a symbol with no implementation keeps
        // the honest "unimplemented" panic below. Multi-result
        // bodyless functions are out of scope for this fallback (their
        // pair convention is not built here) and still panic.
        String symbol = pkgPath + "." + funcName;
        if (graalgo.runtime.Linknames.hasSymbol(symbol)) {
            Object[] args = frame.getArguments();
            Object[] callArgs = java.util.Arrays.copyOfRange(args, 1, args.length);
            return graalgo.runtime.Linknames.dispatch(symbol, callArgs);
        }
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
