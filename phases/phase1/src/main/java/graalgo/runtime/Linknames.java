package graalgo.runtime;

import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

import graalgo.frontend.FrontendException;
import graalgo.values.GoFunc;

/**
 * The {@code //go:linkname} symbol registry (P3-M4, WS C, ADR-0004).
 *
 * <p>Two directions, both by symbol name (SPEC §2.8: "Linkname
 * compatibility is by symbol name, not gc ABI"):
 *
 * <ul>
 *   <li><b>Go → intrinsic.</b> A bodyless Go function carrying
 *       {@code //go:linkname localName symbol} is compiled to a
 *       {@code graalgo.frontend.LinknameRootNode}; calls dispatch here
 *       via {@link #dispatch}. Targets are registered by the launcher
 *       (phase-3 {@code graalgo.p3link.LinknameIntrinsics}); the M4
 *       table maps gc symbol names ({@code runtime.fastrand}, …) to the
 *       intrinsic implementations, so identical Go source links against
 *       gc's runtime under gc and graalgo's intrinsics under graalgo.
 *   <li><b>Intrinsic → Go helper.</b> The launcher binds every compiled
 *       program function under its qualified Go name
 *       ({@code package.name + "." + name}) via {@link #bindGo};
 *       {@link #callGo} looks the {@link GoFunc} up by name and invokes
 *       it through the {@link GoCalls} protocol.
 * </ul>
 *
 * <p>Argument contract: targets receive the call arguments in D5 boxed
 * form ({@code Integer} for int32, {@code Long} for int64, …,
 * {@link graalgo.values.GoString} for strings) with <em>no</em>
 * adaptation by the dispatcher — each registered target adapts its own
 * arguments explicitly and documents the expected D5 types. Arity is
 * checked at dispatch (fail-fast, never silent).
 *
 * <p>verifies: GG-0020
 */
public final class Linknames {
    private Linknames() {}

    /** A linkname target: receives D5-boxed args, returns a D5-boxed result (null for void). */
    @FunctionalInterface
    public interface Target {
        Object invoke(Object[] args);
    }

    private record Registration(Target target, int arity) {}

    private static final Map<String, Registration> TARGETS = new ConcurrentHashMap<>();
    private static final Map<String, GoFunc> GO_FUNCS = new ConcurrentHashMap<>();

    /**
     * Registers a Java intrinsic under {@code symbol}.
     *
     * @param arity the exact number of D5-boxed arguments the target takes;
     *              checked at every dispatch.
     */
    public static void register(String symbol, Target target, int arity) {
        if (symbol == null || target == null) {
            throw new IllegalArgumentException("Linknames.register: null symbol or target");
        }
        if (arity < 0) {
            throw new IllegalArgumentException("Linknames.register: negative arity for " + symbol);
        }
        TARGETS.put(symbol, new Registration(target, arity));
    }

    /** Removes all registrations (tests). */
    public static void clear() {
        TARGETS.clear();
        GO_FUNCS.clear();
    }

    /**
     * Dispatches a linkname call. Unknown symbols and arity mismatches
     * fail fast — a linked call must never silently do the wrong thing.
     */
    public static Object dispatch(String symbol, Object[] args) {
        Registration r = TARGETS.get(symbol);
        if (r == null) {
            throw new FrontendException(
                    "linkname: no intrinsic registered for symbol \"" + symbol + "\"");
        }
        if (args.length != r.arity()) {
            throw new FrontendException("linkname: symbol \"" + symbol + "\" takes "
                    + r.arity() + " arguments, got " + args.length);
        }
        return r.target().invoke(args);
    }

    /** Binds a compiled Go function under its qualified Go name (launcher). */
    public static void bindGo(String qualifiedName, GoFunc fn) {
        if (qualifiedName == null || fn == null) {
            throw new IllegalArgumentException("Linknames.bindGo: null name or function");
        }
        GO_FUNCS.put(qualifiedName, fn);
    }

    /** Looks up a Go function bound by {@link #bindGo}, or null. */
    public static GoFunc resolveGo(String qualifiedName) {
        return GO_FUNCS.get(qualifiedName);
    }

    /**
     * Intrinsic → Go helper by symbol name: invokes the Go function bound
     * under {@code qualifiedName} with the given D5-boxed arguments via
     * the {@link GoCalls} protocol. Unbound names fail fast.
     *
     * <p>Arguments may arrive boxed as {@link graalgo.values.GoInterface}
     * (the call-site adaptation boxes concrete values for
     * {@code any}-typed parameters); {@code GoFuncRootNode} unwraps those
     * for concrete parameters, so the helper sees the D5 values a direct
     * Go call would pass (P3-M4).
     */
    public static Object callGo(String qualifiedName, Object[] args) {
        GoFunc fn = GO_FUNCS.get(qualifiedName);
        if (fn == null) {
            throw new FrontendException(
                    "linkname: no Go function bound under name \"" + qualifiedName + "\"");
        }
        return GoCalls.invoke(fn, args);
    }
}
