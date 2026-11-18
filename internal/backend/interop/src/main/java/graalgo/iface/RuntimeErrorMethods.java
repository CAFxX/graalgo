package graalgo.iface;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.GoFuncRootNode;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoErrorValue;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.PanicRendering;
import graalgo.runtime.RuntimeErrorTypes;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * Method bodies and panic-text resolution for the panic value model
 * (T0.A; SPEC §3.5).
 *
 * <p>(1) The runtime's panic types ({@link RuntimeErrorTypes}) implement
 * {@code error} and {@code runtime.Error} in gc, but have no Go source in
 * graalgo, so the program's method tables lack them. {@link #install}
 * adds Java-implemented {@code Error} and {@code RuntimeError} methods
 * for every runtime error type, so {@code e.Error()} through an
 * {@code error}/{@code runtime.Error} interface (and fmt's error handling)
 * dispatches exactly like any compiled method.
 *
 * <p>(2) gc's {@code preprintpanics} replaces an uncaught panic value by
 * its {@code Error()} (else {@code String()}) text. {@link #install} also
 * registers {@link #methodText} as {@link PanicRendering}'s resolver: it
 * finds the dynamic type's method in the installed tables and runs it on
 * the panicking goroutine.
 */
public final class RuntimeErrorMethods {
    private RuntimeErrorMethods() {}

    /** Installs the runtime error methods and the panic-text resolver. */
    public static void install() {
        GoFunc error = new GoFunc(new MethodRoot(true).getCallTarget(), null);
        GoFunc marker = new GoFunc(new MethodRoot(false).getCallTarget(), null);
        for (String name : RuntimeErrorTypes.typeNames()) {
            IfaceMethodTable.installMethod(name, "Error",
                    new IfaceMethodTarget(error, new int[0], false, false, -1, null));
            IfaceMethodTable.installMethod(name, "RuntimeError",
                    new IfaceMethodTarget(marker, new int[0], false, false, -1, null));
        }
        PanicRendering.setMethodTextResolver(RuntimeErrorMethods::methodText);
    }

    /**
     * The {@code Error() string} (else {@code String() string}) text of a
     * Go value, or null when its dynamic type has neither. Only direct
     * and field-promoted methods with no parameters besides the receiver
     * and a {@code string} result qualify (the {@code error} /
     * {@code fmt.Stringer} signatures).
     */
    static String methodText(Object descriptor, Object value) {
        if (!(descriptor instanceof GoTypeDescriptor d)) {
            return null;
        }
        if (value instanceof GoErrorValue ev) {
            return ev.errorString();
        }
        String t = call(d, value, "Error");
        return t != null ? t : call(d, value, "String");
    }

    private static String call(GoTypeDescriptor d, Object value, String method) {
        IfaceMethodTarget target = IfaceMethodTable.lookup(d.name(), method);
        if (target == null || !target.supported() || target.isEmbeddedRedispatch()) {
            return null;
        }
        if (target.func().target().getRootNode() instanceof GoFuncRootNode root
                && root.paramKinds().length != 1) {
            return null; // not a niladic method: not Error()/String()
        }
        Object recv = target.isDirect()
                ? value
                : graalgo.frontend.SelectorNode.readFieldPath(value, target.recvPath(),
                        !target.pointerReceiver(), (com.oracle.truffle.api.nodes.Node) null);
        if (target.addrOf() && !(recv instanceof GoPointer)) {
            recv = new GoPointer(recv);
        }
        if (target.isDirect() && target.func().target().getRootNode() instanceof GoFuncRootNode root
                && root.paramKinds().length == 1 && root.paramKinds()[0] == GoKind.INTERFACE) {
            recv = new GoInterface(d, value, null);
        }
        GoroutineState state = GoroutineState.current();
        boolean prev = state != null && state.isRecoverAllowed();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        try {
            Object r = GoCalls.invoke(target.func(), new Object[] {recv});
            return r instanceof GoString gs
                    ? new String(gs.bytes(), java.nio.charset.StandardCharsets.UTF_8)
                    : null;
        } finally {
            if (state != null) {
                state.setRecoverAllowed(prev);
            }
        }
    }

    /**
     * {@code Error()} / {@code RuntimeError()} of a runtime error value.
     * The receiver is the {@link GoErrorValue} carrier, or — for
     * string-kind errors after {@code recover()} — its message
     * {@link GoString} (see {@code Panic.recover}).
     */
    private static final class MethodRoot extends RootNode {
        private final boolean error;

        MethodRoot(boolean error) {
            super((TruffleLanguage<?>) null, FrameDescriptor.newBuilder().build());
            this.error = error;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            if (!error) {
                return null;
            }
            Object[] args = frame.getArguments();
            Object recv = args.length > 1 ? args[1] : null;
            if (recv instanceof GoErrorValue ev) {
                return GoString.fromUtf8(ev.errorString());
            }
            if (recv instanceof GoString gs) {
                return gs;
            }
            throw new IllegalStateException("runtime error Error(): unexpected receiver "
                    + (recv == null ? "null" : recv.getClass().getName()));
        }

        @Override
        public String getName() {
            return error ? "runtime.Error" : "runtime.RuntimeError";
        }
    }
}
