package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GcInterop;
import graalgo.values.GoKind;

/**
 * A call to {@code runtime.SetFinalizer(obj, finalizer)} (P2-M4, GC interop —
 * ADR-0002 D4; via the synthetic {@code runtime} import overlay).
 *
 * <p>Evaluates both arguments, then delegates to {@link GcInterop#setFinalizer}.
 * In Phase 1 the target is the stub (throws); Phase 2 shadows it with the
 * {@code Cleaner}-based implementation.
 *
 * <p>Misuse failure modes are gc's, not the stub's: gc {@code throw()}s on
 * every {@code runtime.SetFinalizer} misuse (bad arguments, double-set —
 * {@code src/runtime/mfinal.go}, verified against go1.27.1), i.e. fatal,
 * unrecoverable, {@code fatal error: <msg>} on stderr, exit 2. The
 * {@link GcInterop} signals carry gc's exact message text
 * ({@code IllegalArgumentException}/{@code IllegalStateException}); this
 * node translates them to the fatal path — letting the raw Java
 * exceptions propagate would surface as {@code graalgo-run: execution
 * failed} / exit 1, and a Go panic would be (wrongly) recoverable.
 */
public final class SetFinalizerCallNode extends GoExprNode {
    @Children private final GoExprNode[] args;

    public SetFinalizerCallNode(GoExprNode[] args) {
        super(GoKind.INTERFACE);
        this.args = args;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object obj = args[0].executeObject(frame);
        Object finalizer = args[1].executeObject(frame);
        try {
            GcInterop.setFinalizer(obj, finalizer);
        } catch (IllegalArgumentException | IllegalStateException e) {
            // gc throw()s on every SetFinalizer misuse (bad arguments,
            // double-set): fatal, unrecoverable — a Go recover() must NOT
            // succeed where gc dies. The signal carries gc's message text;
            // translate it to the fatal path here (the same signal/binding
            // split as sync misuse, ADR-0002 D4).
            fatal(e.getMessage());
        }
        return null;
    }

    /**
     * gc's {@code throw()} for {@code runtime.SetFinalizer} misuse:
     * {@code fatal error: <msg>} on stderr, exit 2, unrecoverable from Go
     * code. Module-local: the truffle module cannot depend on the interop
     * module's {@code SyncRuntime}; the observable contract is identical
     * ({@code fatal error: <msg>} + exit 2).
     */
    private static void fatal(String msg) {
        System.err.println("fatal error: " + msg);
        System.err.flush();
        System.exit(2);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        executeObject(frame);
        return 0;
    }
}
