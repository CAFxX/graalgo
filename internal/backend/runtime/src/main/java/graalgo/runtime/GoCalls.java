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
     * <p>TR-8 (decided semantics): every entry into compiled Go code runs
     * with a non-null {@link GoroutineState} on the calling thread. Calls
     * originating on a goroutine (the common case — direct calls, method
     * values, interface dispatch, linkname intrinsics invoked from Go)
     * reuse that goroutine's state. Calls from Java-driven threads that
     * carry no state (finalizers on the Cleaner thread, pool cleanup,
     * Java-side {@link Linknames#callGo} invocations, reflect-driven calls
     * from Java) install a fresh thread-confined state for the duration of
     * the call and restore the previous holder afterwards — so
     * {@code defer}, {@code panic} and {@code recover} behave exactly as
     * on a goroutine instead of dying on a null dereference or silently
     * dropping defers. A fresh state is installed <em>only</em> when none
     * is present; an existing goroutine state is never shadowed (its Go
     * stack, in-flight panic and recover flags stay visible to the callee).
     *
     * @param fn   the function value (non-null; a nil func value is a Go
     *             panic — callers check first).
     * @param args the call arguments in D5 mapping.
     * @return the call's single result, or null for void calls.
     */
    public static Object invoke(GoFunc fn, Object[] args) {
        if (GoroutineState.current() != null) {
            return fn.target().call(callArgs(fn, args));
        }
        // Off-goroutine entry: install a fresh thread-confined state so
        // defer/panic/recover work; restore afterwards so a pooled Java
        // thread never keeps a stale state (a stale holder would make a
        // later unrelated call observe a dirty defer chain).
        GoroutineState prev = GoroutineState.install(new GoroutineState());
        try {
            return fn.target().call(callArgs(fn, args));
        } finally {
            GoroutineState.restore(prev);
        }
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
