package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.go.types.GoType;
import graalgo.go.types.Method;
import graalgo.go.types.TypeTable;
import graalgo.go.values.Iface;
import graalgo.go.values.Str;
import graalgo.language.GoLanguage;
import graalgo.runtime.GoErrorValue;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Function;
import java.util.function.Supplier;

/**
 * The language-level type universe: the {@link TypeTable} and the runtime's own error types
 * (the panic value model of {@code graalgo.runtime.RuntimeErrorTypes}: {@code runtime.errorString},
 * {@code runtime.plainError}, {@code runtime.boundsError}, {@code *runtime.TypeAssertionError},
 * {@code *runtime.PanicNilError}). Until the facade {@code runtime} package (toolexec W5)
 * declares them in Go, they are defined here with Java-implemented {@code Error()} and
 * {@code RuntimeError()} methods, so a recovered runtime panic satisfies {@code error} and
 * {@code runtime.Error} like any Go value. Their data word is the {@link GoErrorValue}.
 */
public final class RuntimeTypes {
    /** A Java-implemented Go function: arguments in calling-convention form. */
    static final class NativeRoot extends RootNode {
        private final String name;
        private final Function<Object[], Object> body;

        NativeRoot(GoLanguage language, String name, Function<Object[], Object> body) {
            super(language);
            this.name = name;
            this.body = body;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            return call(frame.getArguments());
        }

        @TruffleBoundary
        private Object call(Object[] args) {
            return body.apply(args);
        }

        @Override
        public String getName() {
            return name;
        }

        @Override
        public boolean isInternal() {
            return true;
        }
    }

    public final TypeTable table = new TypeTable();
    private final GoLanguage language;
    private final Map<String, GoType> errorTypes = new HashMap<>();
    private final Map<String, RootCallTarget> shared = new ConcurrentHashMap<>();

    public RuntimeTypes(GoLanguage language) {
        this.language = language;
        for (String name : new String[] {"errorString", "plainError", "boundsError"}) {
            errorTypes.put("runtime." + name, define(name, false));
        }
        for (String name : new String[] {"TypeAssertionError", "PanicNilError"}) {
            errorTypes.put("*runtime." + name, table.pointer(define(name, true)));
        }
    }

    private GoType define(String name, boolean pointer) {
        GoType[] self = new GoType[1];
        Map<String, Method> methods = new HashMap<>();
        // Identity-compared opaque data word: the underlying type only has to be a reference.
        GoType t = table.named("runtime", name, new GoType[0], () -> table.basic(GoType.UNSAFE_POINTER),
                () -> pointer ? Map.of() : methods, () -> methods);
        self[0] = t;
        RootCallTarget error = new NativeRoot(language, "runtime." + name + ".Error",
                args -> Str.of(((GoErrorValue) args[1]).errorString())).getCallTarget();
        RootCallTarget marker = new NativeRoot(language, "runtime." + name + ".RuntimeError", args -> null)
                .getCallTarget();
        GoType errorSig = table.func(new GoType[0], new GoType[] {table.string}, false);
        GoType markerSig = table.func(new GoType[0], new GoType[0], false);
        methods.put("Error", new Method("Error", new int[0], pointer, t, pointer, () -> error, () -> errorSig));
        methods.put("RuntimeError", new Method("RuntimeError", new int[0], pointer, t, pointer, () -> marker,
                () -> markerSig));
        return t;
    }

    /** The interface value of a runtime error. */
    @TruffleBoundary
    public Iface box(GoErrorValue e) {
        GoType t = e.typeName() == null ? null : errorTypes.get(e.typeName());
        return new Iface(t != null ? t : errorTypes.get("runtime.errorString"), e);
    }

    /**
     * The one call target of a Java-implemented function shared by every func value it backs
     * (an intrinsic that returns a closure: the value differs per call in its environment, not
     * in its code), built by {@code make} on first use.
     */
    public RootCallTarget sharedTarget(String name, Supplier<RootNode> make) {
        return shared.computeIfAbsent(name, k -> make.get().getCallTarget());
    }

    /** A Java-implemented function as a call target (intrinsics, D9). */
    public RootCallTarget nativeTarget(String name, Function<Object[], Object> body) {
        return new NativeRoot(language, name, body).getCallTarget();
    }
}
