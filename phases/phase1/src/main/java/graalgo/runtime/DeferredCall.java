package graalgo.runtime;

import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * A deferred call: the function value plus its already-evaluated arguments,
 * captured when the {@code defer} statement executes (M5, WS F).
 *
 * <p>Go evaluates the deferred function value and its arguments immediately
 * and invokes the call when the surrounding function returns (or unwinds).
 * A {@code DeferredCall} is that captured unit: {@link #invoke} performs
 * the deferred invocation.
 *
 * <p>Two kinds: {@link Kind#FUNC} (a {@link GoFunc} value) and
 * {@link Kind#BUILTIN} (a builtin call by name: {@code println},
 * {@code print}, {@code panic}, {@code recover}). The kind drives
 * {@code recover()} semantics: only a directly-deferred function enables
 * recovery; deferred builtins run with recovery disarmed (gc-verified).
 */
public final class DeferredCall {
    /** Whether the deferred call is a function value or a builtin. */
    public enum Kind { FUNC, BUILTIN }

    private final Kind kind;
    private final GoFunc func;
    private final Object[] args;
    private final GoKind[] argKinds;
    private final String builtinName;

    private DeferredCall(Kind kind, GoFunc func, Object[] args, GoKind[] argKinds, String builtinName) {
        this.kind = kind;
        this.func = func;
        this.args = args;
        this.argKinds = argKinds;
        this.builtinName = builtinName;
    }

    /** A deferred function-value call. */
    public static DeferredCall func(GoFunc func, Object[] args) {
        return new DeferredCall(Kind.FUNC, func, args, null, null);
    }

    /** A deferred builtin call ({@code println}, {@code print}, {@code panic}, {@code recover}). */
    public static DeferredCall builtin(String name, Object[] args, GoKind[] argKinds) {
        return new DeferredCall(Kind.BUILTIN, null, args, argKinds, name);
    }

    public Kind kind() {
        return kind;
    }

    /** Invokes the captured deferred call. */
    public void invoke() {
        switch (kind) {
            case FUNC -> GoCalls.invoke(func, args);
            case BUILTIN -> invokeBuiltin();
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
}
