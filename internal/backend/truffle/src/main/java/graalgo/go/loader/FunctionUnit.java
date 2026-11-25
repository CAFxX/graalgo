package graalgo.go.loader;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import graalgo.gir.Gir;
import graalgo.go.build.NodeFactory;
import graalgo.go.types.GoType;
import graalgo.go.values.Closure;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Supplier;

/**
 * One function of a package (toolexec-design §4.3): its body blob stays undecoded until the
 * first call asks an {@link Instance} for its call target. A generic function has one instance
 * per type-argument tuple, each with its own Truffle tree (D6); every other function has one.
 */
public final class FunctionUnit {
    public final PackageUnit pkg;
    public final int index;
    public final Gir.Func func;
    /** gc's symbol name: {@code pkg.F}, {@code pkg.T.M}, {@code pkg.(*T).M}. */
    public final String symbol;
    private final ConcurrentHashMap<String, Instance> instances = new ConcurrentHashMap<>();

    FunctionUnit(PackageUnit pkg, int index) {
        this.pkg = pkg;
        this.index = index;
        this.func = pkg.gir.sections().funcs[index];
        this.symbol = pkg.path + "." + pkg.str(func.name());
    }

    /** An instantiation: the function under a type environment (null: not generic). */
    public final class Instance implements Supplier<RootCallTarget> {
        public final TypeEnv env;
        private volatile RootCallTarget target;
        private volatile Closure closure;

        Instance(TypeEnv env) {
            this.env = env;
        }

        public FunctionUnit unit() {
            return FunctionUnit.this;
        }

        /** The call target; decodes the body and builds the tree on first use. */
        @Override
        public RootCallTarget get() {
            RootCallTarget t = target;
            return t != null ? t : build();
        }

        @TruffleBoundary
        private synchronized RootCallTarget build() {
            if (target == null) {
                RootCallTarget bound = pkg.program.bound(FunctionUnit.this);
                target = bound != null ? bound : NodeFactory.buildFunction(this);
            }
            return target;
        }

        /** The function as a func value (no environment). */
        @TruffleBoundary
        public Closure closure() {
            Closure c = closure;
            if (c == null) {
                c = new Closure(get(), null);
                closure = c;
            }
            return c;
        }
    }

    /**
     * gc's name of the function in tracebacks and {@code runtime.FuncForPC}: the symbol, with
     * {@code [...]} after a generic function's name or a generic receiver's type name
     * ({@code pkg.F[...]}, {@code pkg.(*T[...]).M}).
     */
    public String traceName() {
        if (func.tparams().length == 0) {
            return symbol;
        }
        String local = pkg.str(func.name());
        int dot = local.lastIndexOf('.');
        if (dot < 0) {
            return pkg.path + "." + local + "[...]";
        }
        String recv = local.substring(0, dot);
        String method = local.substring(dot);
        recv = recv.endsWith(")") ? recv.substring(0, recv.length() - 1) + "[...])" : recv + "[...]";
        return pkg.path + "." + recv + method;
    }

    public boolean isBodyless() {
        return (func.flags() & 1) != 0;
    }

    /** The instance for the given type arguments (receiver type arguments first for methods). */
    @TruffleBoundary
    public Instance instance(GoType[] targs) {
        if (func.tparams().length == 0) {
            return instances.computeIfAbsent("", k -> new Instance(null));
        }
        if (targs.length != func.tparams().length) {
            throw new IllegalStateException(symbol + ": " + targs.length + " type arguments for "
                    + func.tparams().length + " type parameters");
        }
        String[] keys = new String[targs.length];
        for (int i = 0; i < keys.length; i++) {
            keys[i] = pkg.typeKey(func.tparams()[i]);
        }
        TypeEnv env = new TypeEnv(keys, targs);
        return instances.computeIfAbsent(env.id, k -> new Instance(env));
    }
}
