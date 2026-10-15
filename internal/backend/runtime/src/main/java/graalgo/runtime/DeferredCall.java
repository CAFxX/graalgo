package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;

import graalgo.frontend.FmtCallNode;
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
    // FMT-kind: the overlay function name plus the pre-built Fmt.Arg array
    // (values evaluated at defer time, metadata from FmtCallNode). The
    // formatting replays via FmtCallNode.executeFmt at return/unwind time.
    private final String fmtFunc;
    private final Fmt.Arg[] fmtArgs;

    private DeferredCall(Kind kind, GoFunc func, Object[] args, GoKind[] argKinds, String builtinName,
            String fmtFunc, Fmt.Arg[] fmtArgs) {
        this.kind = kind;
        this.func = func;
        this.args = args;
        this.argKinds = argKinds;
        this.builtinName = builtinName;
        this.fmtFunc = fmtFunc;
        this.fmtArgs = fmtArgs;
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
        return new DeferredCall(Kind.FUNC, func, args, null, null, null, null);
    }

    /** A deferred builtin call ({@code println}, {@code print}, {@code panic}, {@code recover}). */
    public static DeferredCall builtin(String name, Object[] args, GoKind[] argKinds) {
        return new DeferredCall(Kind.BUILTIN, null, args, argKinds, name, null, null);
    }

    /**
     * A deferred {@code fmt} overlay call (M7). The {@code Fmt.Arg} array is
     * built at defer time (values evaluated, metadata attached); the
     * formatting replays via {@link FmtCallNode#executeFmt} at return/unwind
     * time. Results are discarded (defer is statement position).
     */
    public static DeferredCall fmtCall(String funcName, Fmt.Arg[] fargs) {
        return new DeferredCall(Kind.FMT, null, null, null, null, funcName, fargs);
    }

    public Kind kind() {
        return kind;
    }

    /**
     * PKG-039: global push order of this deferred call, stamped by
     * {@link DeferChain#push}. Range-over-func bodies stash their defers
     * in the enclosing frame (gc's {@code deferrangefunc}); at unwind
     * the stashed calls are merged back with the chain by this order so
     * interleaved defers run in true LIFO push order.
     */
    private long seqno;

    /** Stamps the push order (called by {@link DeferChain#push}). */
    void setSeqno(long seqno) {
        this.seqno = seqno;
    }

    /** The push order stamped at {@link DeferChain#push} time. */
    long seqno() {
        return seqno;
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
            // FMT: replay via the shared FmtCallNode dispatch; results are
            // discarded (defer is statement position).
            case FMT -> FmtCallNode.executeFmt(fmtFunc, fmtArgs);
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
