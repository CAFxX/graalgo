package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;

import graalgo.values.CallSiteProbes;
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

    /**
     * P3-M4 remediation (P1-2): the AST node whose execution created this
     * deferred call (the {@code defer} statement). A deferred
     * {@code panic(x)} throws with this as the throw site so the panic
     * renderer attributes the innermost frame to the defer statement
     * (gc's line), not the function definition line. Null when unset.
     */
    private Node callSite;

    /** Attaches the call site (called by DeferNode at defer time). */
    public void setCallSite(Node site) {
        this.callSite = site;
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
        }
    }

    private void invokeBuiltin() {
        switch (builtinName) {
            case "println" -> BuiltinPrint.println(toArgs());
            case "print" -> BuiltinPrint.print(toArgs());
            case "panic" -> Panic.throwPanic(args[0], null, null, callSite);
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
