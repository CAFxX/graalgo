package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.go.types.GoType;
import graalgo.go.types.Method;
import graalgo.go.types.TypeTable;
import graalgo.go.values.Flat;
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
 * The language-level type universe: the {@link TypeTable} and the runtime's error types. The
 * exported ones, {@code *runtime.TypeAssertionError} and {@code *runtime.PanicNilError}, are the
 * facade runtime's Go declarations (stdlib/fork/runtime/error.go, BUG-0046), instantiated here
 * from the running program's {@code runtime} package ({@link #attachRuntime}). The unexported
 * ones of the panic value model of {@code graalgo.runtime.RuntimeErrorTypes} ({@code
 * runtime.errorString}, {@code runtime.plainError}, {@code runtime.boundsError}) are defined here
 * with Java-implemented {@code Error()} and {@code RuntimeError()} methods, so a recovered
 * runtime panic satisfies {@code error} and {@code runtime.Error} like any Go value; their data
 * word is the {@link GoErrorValue}.
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
    /** The defined types of the running program's {@code runtime} package, by name. */
    private volatile Function<String, GoType> runtimePackage;

    public RuntimeTypes(GoLanguage language) {
        this.language = language;
        for (String name : new String[] {"errorString", "plainError", "boundsError"}) {
            errorTypes.put("runtime." + name, define(name));
        }
    }

    /** A value-receiver error type whose data word is the {@link GoErrorValue}. */
    private GoType define(String name) {
        Map<String, Method> methods = new HashMap<>();
        // Identity-compared opaque data word: the underlying type only has to be a reference.
        GoType t = table.named("runtime", name, new GoType[0], () -> table.basic(GoType.UNSAFE_POINTER),
                () -> methods, () -> methods);
        RootCallTarget error = new NativeRoot(language, "runtime." + name + ".Error",
                args -> Str.of(((GoErrorValue) args[1]).errorString())).getCallTarget();
        RootCallTarget marker = new NativeRoot(language, "runtime." + name + ".RuntimeError", args -> null)
                .getCallTarget();
        GoType errorSig = table.func(new GoType[0], new GoType[] {table.string}, false);
        GoType markerSig = table.func(new GoType[0], new GoType[0], false);
        methods.put("Error", new Method("Error", new int[0], false, t, false, () -> error, () -> errorSig));
        methods.put("RuntimeError", new Method("RuntimeError", new int[0], false, t, false, () -> marker,
                () -> markerSig));
        return t;
    }

    /**
     * Attaches the running program's {@code runtime} package: {@code declared} returns the
     * defined type it declares under a name (one program runs at a time per process).
     */
    public void attachRuntime(Function<String, GoType> declared) {
        runtimePackage = declared;
    }

    private GoType runtimeType(String name) {
        Function<String, GoType> declared = runtimePackage;
        if (declared == null) {
            throw new IllegalStateException("no runtime package attached: the program was not linked with the facade runtime");
        }
        return declared.apply(name);
    }

    /**
     * gc's {@code &TypeAssertionError{iface, concrete, asserted, missingMethod}} as an {@code any}:
     * the facade's struct with the types' descriptors ({@code iface}, {@code concrete} null for
     * none).
     */
    @TruffleBoundary
    public Iface typeAssertionError(GoType iface, GoType concrete, GoType asserted, String missingMethod) {
        GoType t = runtimeType("TypeAssertionError");
        Flat e = t.zeroFlat();
        GoType.Field[] f = t.u().fields;
        e.refs[f[0].refOff] = iface == null ? null : table.descriptors.typeWord(iface);
        e.refs[f[1].refOff] = concrete == null ? null : table.descriptors.typeWord(concrete);
        e.refs[f[2].refOff] = table.descriptors.typeWord(asserted);
        e.refs[f[3].refOff] = Str.of(missingMethod);
        return new Iface(table.pointer(t), e);
    }

    /** gc's {@code new(PanicNilError)} as an {@code any}. */
    @TruffleBoundary
    public Iface panicNilError() {
        GoType t = runtimeType("PanicNilError");
        return new Iface(table.pointer(t), t.zeroFlat());
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
