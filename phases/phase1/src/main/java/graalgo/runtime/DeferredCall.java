package graalgo.runtime;

import graalgo.values.CallSiteProbes;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * A deferred call: the function value plus its already-evaluated arguments,
 * captured when the {@code defer} statement executes (M5, WS F).
 *
 * <p>Go evaluates the deferred function value and its arguments immediately
 * and invokes the call when the surrounding function returns (or unwinds).
 * A {@code DeferredCall} is that captured unit: {@link #invoke} performs
 * the deferred invocation.
 *
 * <p>Three kinds: {@link Kind#FUNC} (a {@link GoFunc} value),
 * {@link Kind#BUILTIN} (a builtin call by name: {@code println},
 * {@code print}, {@code panic}, {@code recover}), and {@link Kind#FMT}
 * (a deferred {@code fmt} overlay call: argument values captured at
 * defer time, formatting replayed at return/unwind time). The kind
 * drives {@code recover()} semantics: only a directly-deferred function
 * enables recovery; deferred builtins and fmt calls run with recovery
 * disarmed (gc-verified).
 */
public final class DeferredCall {
    /** Whether the deferred call is a function value, a builtin, or a fmt call. */
    public enum Kind { FUNC, BUILTIN, FMT }

    private final Kind kind;
    private final GoFunc func;
    private final Object[] args;
    private final GoKind[] argKinds;
    private final String builtinName;
    // FMT-kind metadata: the overlay function name plus per-argument static
    // type metadata (parallel to FmtCallNode's fields). argVals are the
    // already-evaluated argument values, captured at defer time.
    private final String fmtFunc;
    private final String[] fmtTypeNames;
    private final GoKind[] fmtElemKinds;
    private final String[] fmtElemTypeNames;
    private final java.util.List<GoKind>[] fmtElemKindChains;
    private final java.util.List<String>[] fmtElemTypeNameChains;
    private final GoKind[] fmtKeyKinds;
    private final String[] fmtKeyTypeNames;
    private final java.util.List<GoKind>[] fmtKeyKindChains;
    private final java.util.List<String>[] fmtKeyTypeNameChains;

    private DeferredCall(Kind kind, GoFunc func, Object[] args, GoKind[] argKinds, String builtinName,
            String fmtFunc, String[] fmtTypeNames, GoKind[] fmtElemKinds, String[] fmtElemTypeNames,
            java.util.List<GoKind>[] fmtElemKindChains,
            java.util.List<String>[] fmtElemTypeNameChains,
            GoKind[] fmtKeyKinds, String[] fmtKeyTypeNames,
            java.util.List<GoKind>[] fmtKeyKindChains,
            java.util.List<String>[] fmtKeyTypeNameChains) {
        this.kind = kind;
        this.func = func;
        this.args = args;
        this.argKinds = argKinds;
        this.builtinName = builtinName;
        this.fmtFunc = fmtFunc;
        this.fmtTypeNames = fmtTypeNames;
        this.fmtElemKinds = fmtElemKinds;
        this.fmtElemTypeNames = fmtElemTypeNames;
        this.fmtElemKindChains = fmtElemKindChains;
        this.fmtElemTypeNameChains = fmtElemTypeNameChains;
        this.fmtKeyKinds = fmtKeyKinds;
        this.fmtKeyTypeNames = fmtKeyTypeNames;
        this.fmtKeyKindChains = fmtKeyKindChains;
        this.fmtKeyTypeNameChains = fmtKeyTypeNameChains;
    }

    /**
     * M9 (P1-2): polymorphism probe site for a deferred function-value call,
     * attached by {@code DeferNode} (the defer statement's site). Null when
     * probing is disabled or the call is not a function value.
     */
    private CallSiteProbes.Site probeSite;

    /** Attaches the probe site (called by DeferNode at defer time). */
    public void setProbeSite(CallSiteProbes.Site site) {
        this.probeSite = site;
    }

    /** A deferred function-value call. */
    public static DeferredCall func(GoFunc func, Object[] args) {
        return new DeferredCall(Kind.FUNC, func, args, null, null, null, null, null, null, null, null, null, null, null, null);
    }

    /** A deferred builtin call ({@code println}, {@code print}, {@code panic}, {@code recover}). */
    public static DeferredCall builtin(String name, Object[] args, GoKind[] argKinds) {
        return new DeferredCall(Kind.BUILTIN, null, args, argKinds, name, null, null, null, null, null, null, null, null, null, null);
    }

    /** A deferred {@code fmt} overlay call (M7): values captured at defer time. */
    public static DeferredCall fmtCall(String funcName, Object[] argVals, GoKind[] argKinds,
            String[] typeNames, GoKind[] elemKinds, String[] elemTypeNames,
            java.util.List<GoKind>[] elemKindChains,
            java.util.List<String>[] elemTypeNameChains,
            GoKind[] keyKinds, String[] keyTypeNames,
            java.util.List<GoKind>[] keyKindChains,
            java.util.List<String>[] keyTypeNameChains) {
        return new DeferredCall(Kind.FMT, null, argVals, argKinds, null, funcName, typeNames,
                elemKinds, elemTypeNames, elemKindChains, elemTypeNameChains, keyKinds, keyTypeNames,
                keyKindChains, keyTypeNameChains);
    }

    public Kind kind() {
        return kind;
    }

    /** Invokes the captured deferred call. */
    public void invoke() {
        switch (kind) {
            case FUNC -> {
                // M9 (P1-2): deferred function-value calls are real call
                // sites; record the compiled target for the polymorphism
                // histogram when probing is enabled.
                if (CallSiteProbes.isEnabled() && probeSite != null) {
                    probeSite.recordTarget(func.target());
                }
                GoCalls.invoke(func, args);
            }
            case BUILTIN -> invokeBuiltin();
            case FMT -> invokeFmt();
        }
    }

    private void invokeBuiltin() {
        switch (builtinName) {
            case "println" -> BuiltinPrint.println(toArgs());
            case "print" -> BuiltinPrint.print(toArgs());
            case "panic" -> Panic.throwPanic(args[0]);
            case "recover" -> Panic.recover(); // result discarded; never stops the panic
            default -> throw new IllegalStateException("cannot defer builtin " + builtinName + " (WS G)");
        }
    }

    private BuiltinPrint.Arg[] toArgs() {
        BuiltinPrint.Arg[] out = new BuiltinPrint.Arg[args.length];
        for (int i = 0; i < args.length; i++) {
            out[i] = new BuiltinPrint.Arg(argKinds[i], args[i]);
        }
        return out;
    }

    /**
     * Replays a deferred {@code fmt} overlay call from the values captured
     * at defer time. Mirrors FmtCallNode's dispatch; Sprint-variant results
     * are discarded (defer is statement position).
     */
    private void invokeFmt() {
        Fmt.Arg[] fargs = new Fmt.Arg[args.length];
        for (int i = 0; i < args.length; i++) {
            fargs[i] = new Fmt.Arg(args[i], argKinds[i], fmtTypeNames[i],
                    fmtKeyKinds[i], fmtKeyTypeNames[i], fmtElemKinds[i], fmtElemTypeNames[i],
                    fmtElemKindChains[i], fmtElemTypeNameChains[i],
                    fmtKeyKindChains[i], fmtKeyTypeNameChains[i]);
        }
        switch (fmtFunc) {
            case "Print" -> Fmt.print(fargs);
            case "Printf" -> Fmt.printf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            case "Println" -> Fmt.println(fargs);
            case "Sprint" -> Fmt.sprint(fargs);
            case "Sprintf" -> Fmt.sprintf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            case "Sprintln" -> Fmt.sprintln(fargs);
            case "Errorf" -> Fmt.errorf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            default -> throw new IllegalStateException("unknown fmt function: " + fmtFunc);
        }
    }

    private String fmtString(Fmt.Arg[] fargs) {
        if (fargs.length == 0) {
            throw new IllegalStateException(fmtFunc + " requires a format string");
        }
        Object v = fargs[0].value();
        if (!(v instanceof GoString gs)) {
            throw new IllegalStateException(fmtFunc + " format is not a string");
        }
        return new String(gs.bytes(), java.nio.charset.StandardCharsets.UTF_8);
    }
}
