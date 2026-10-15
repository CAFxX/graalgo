package graalgo.runtime;

import com.oracle.truffle.api.RootCallTarget;
import graalgo.values.GoFunc;

/**
 * Invoking Go function values (M5, WS F).
 *
 * <p>Calling convention for function literals (and, later, named functions):
 * the callee's {@link RootCallTarget} is invoked with the {@link GoFunc}
 * itself as the first argument, followed by the call arguments. The root
 * node reads the {@code GoFunc} from the arguments to install the closure
 * environment ({@code $closure} frame slot) before running the body.
 */
public final class GoCalls {
    private GoCalls() {}

    /**
     * Builds the callee argument array for a Go function value call.
     *
     * <p>Calling convention: the callee's {@link RootCallTarget} is invoked
     * with the {@link GoFunc} itself as the first argument, followed by the
     * call arguments. A method value binds its receiver in boundArgs;
     * it is prepended so the callee sees [fn, receiver, ...args], exactly
     * as for a direct methodref call (P3-M9, WS H).
     *
     * @param fn   the function value (non-null)
     * @param args the call arguments in D5 mapping
     * @return the full argument array for {@code fn.target().call(...)}
     */
    public static Object[] callArgs(GoFunc fn, Object[] args) {
        Object[] bound = fn.boundArgs();
        int prefix = bound == null ? 0 : bound.length;
        Object[] callArgs = new Object[args.length + prefix + 1];
        callArgs[0] = fn;
        if (bound != null) {
            System.arraycopy(bound, 0, callArgs, 1, bound.length);
        }
        System.arraycopy(args, 0, callArgs, 1 + prefix, args.length);
        return callArgs;
    }

    /**
     * Invokes a Go function value with already-evaluated arguments.
     *
     * @param fn   the function value (non-null; a nil func value is a Go
     *             panic — callers check first).
     * @param args the call arguments in D5 mapping.
     * @return the call's single result, or null for void calls.
     */
    public static Object invoke(GoFunc fn, Object[] args) {
        return fn.target().call(callArgs(fn, args));
    }

    /**
     * The Go panic for calling a nil function value: gc reports
     * {@code runtime error: invalid memory address or nil pointer
     * dereference}.
     */
    public static GoErrorValue nilFuncCall() {
        // P0-fmt-016: gc panics with runtime.errorString here.
        return GoErrors.nilDereference();
    }
}
