package graalgo.frontend;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;

import graalgo.runtime.Panic;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoString;

/**
 * Root node for a bodyless function with no provider in graalgo: a
 * bodyless method (which in gc is normally backed by an assembly body
 * that graalgo does not process, and which cannot be symbol-dispatched
 * because methods bind through method tables), a multi-result
 * declaration under gc's default symbol (linkname dispatch carries a
 * single result and a defaulted declaration never aliases to a
 * compiled provider), or a multi-result {@code //go:linkname}
 * declaration with no compiled provider.
 *
 * <p>Calling convention (same as {@link GoFuncRootNode}): {@code args[0]}
 * is the {@link graalgo.values.GoFunc}, {@code args[1..]} are the call
 * arguments in D5 boxed form. The call panics with an "unimplemented"
 * message naming the actual reason the declaration has no
 * implementation (P1-3: the old message blamed assembly for every
 * shape, including bodyless methods and missing linkname providers
 * that have nothing to do with assembly). This is general: it applies
 * uniformly to all bodyless functions, preserving their type identity
 * so references and value positions work, while failing loudly (never
 * silently) if the unimplemented function is actually invoked.
 *
 * <p>This is not a bridge: it does not pretend to implement the function.
 * It explicitly marks the function as unimplemented. It is NOT the
 * equivalent of gc's link error: gc fails the BUILD for a missing
 * implementation, while this node builds successfully and panics only
 * if invoked. The build-time equivalent is the frontend's reachability
 * gate (checkStubReachability), which fails the export when a stub is
 * referenced from reachable code; the stubs that survive it are
 * unreachable in the emitted program, except through interface
 * dispatch without an explicit conversion node and through reflect,
 * which the gate cannot see (documented in docs/arch/limitations.md).
 */
public final class UnimplementedRootNode extends RootNode {
    private final String funcName;
    private final String pkgPath;
    private final String reason;
    private SourceSection sourceSection;

    public UnimplementedRootNode(String funcName, String pkgPath) {
        this(funcName, pkgPath, "");
    }

    public UnimplementedRootNode(String funcName, String pkgPath, String reason) {
        super((TruffleLanguage<?>) null, FrameDescriptor.newBuilder().build());
        this.funcName = funcName;
        this.pkgPath = pkgPath;
        this.reason = reason == null ? "" : reason;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Safepoint.poll();
        String msg = "unimplemented: " + pkgPath + "." + funcName
                + " (" + (reason.isEmpty()
                        ? "no implementation is available in graalgo" : reason) + ")";
        // A GoString value, not a java.lang.String: PanicRendering only
        // renders GoString/GoErrorValue panic values faithfully — a raw
        // String degraded to "panic: (?) 0xADDR", hiding the message
        // (fix-redmaster-r41).
        Panic.throwPanic(GoString.fromUtf8(msg), null, null, this);
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
