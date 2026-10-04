package graalgo.runtime;

import java.util.ArrayDeque;
import java.util.Deque;

/**
 * A goroutine's chain of deferred calls (M5, WS F; SPEC §3.5).
 *
 * <p>The chain lives in the {@link GoroutineState} (one per goroutine; Phase 1
 * has only the main goroutine). Each <em>function invocation</em> records a
 * watermark ({@link #depth()}) on entry; unwinding drains only the calls
 * pushed after the watermark, so nested calls get per-function defer
 * semantics from the single per-goroutine chain.
 *
 * <p>The frozen cross-workstream surface is {@link #push} and
 * {@link #runAll}; the watermark unwind methods are additive.
 */
public final class DeferChain {
    private final Deque<DeferredCall> chain = new ArrayDeque<>();

    /** Pushes a deferred call (frozen). Evaluated arguments are captured. */
    public void push(DeferredCall call) {
        chain.push(call);
    }

    /** Current depth: a function entry records this as its watermark. */
    public int depth() {
        return chain.size();
    }

    /**
     * Runs all deferred calls, LIFO (frozen). Used when the goroutine ends;
     * function returns use {@link #unwindReturning(int)} with their watermark.
     */
    public void runAll() {
        unwindReturning(0);
    }

    /**
     * Runs deferred calls down to (not including) {@code mark}, LIFO, for a
     * normal function return. Deferred calls see the function's named return
     * values as the body left them (defer runs before the return completes).
     */
    public void unwindReturning(int mark) {
        while (chain.size() > mark) {
            DeferredCall dc = chain.pop();
            GoroutineState state = GoroutineState.current();
            if (state != null) {
                state.setRecoverAllowed(false);
            }
            dc.invoke();
        }
    }

    /**
     * Runs deferred calls down to {@code mark} during a panic unwind.
     *
     * @param mark the unwinding function's watermark.
     * @param initial the panic in flight.
     * @return the panic to continue unwinding with, or null if a deferred
     *         call recovered it (the caller then resumes after the chain).
     *
     * <p>Semantics (all gc-verified): a nested panic replaces the in-flight
     * one (remaining defers still run); a {@link Goexit} abandons the panic
     * and runs the remaining defers as a goexit unwind; a recovered panic
     * stops the unwind <em>but the remaining deferred calls still run</em>
     * (their {@code recover()} calls see nil), then the function resumes
     * normally.
     */
    public PanicException unwindPanicking(int mark, PanicException initial) {
        GoroutineState state = GoroutineState.current();
        PanicException prev = state != null ? state.panicking() : null;
        if (state != null) {
            state.setPanicking(initial);
        }
        try {
            PanicException current = initial;
            while (chain.size() > mark) {
                DeferredCall dc = chain.pop();
                if (state != null) {
                    // recover() is non-nil only when called directly by a
                    // deferred function while a panic is in flight: arm the
                    // flag for function-literal defers, disarm for deferred
                    // builtins — and disarm entirely once the panic is
                    // recovered (remaining defers see nil from recover()).
                    state.setRecoverAllowed(
                            current != null && dc.kind() == DeferredCall.Kind.FUNC);
                    state.setPanicking(current);
                }
                try {
                    dc.invoke();
                } catch (PanicException nested) {
                    // Nested panic replaces the in-flight one; remaining
                    // defers run against the new panic.
                    current = nested;
                    if (state != null) {
                        state.setPanicking(nested);
                    }
                } catch (Goexit g) {
                    // Goexit during panic unwind abandons the panic (gc):
                    // run the remaining defers as a goexit unwind.
                    unwindGoexiting(mark);
                    throw g;
                } finally {
                    if (state != null) {
                        state.setRecoverAllowed(false);
                    }
                }
                if (current != null && current.isRecovered()) {
                    // Recovered: the panic stops, but the remaining
                    // deferred calls still run (gc-verified); their
                    // recover() calls observe nil.
                    current = null;
                }
            }
            return current;
        } finally {
            if (state != null) {
                state.setPanicking(prev);
            }
        }
    }

    /**
     * Runs deferred calls down to {@code mark} during a {@link Goexit}.
     * Not recoverable: the recover flag stays disarmed throughout.
     */
    public void unwindGoexiting(int mark) {
        GoroutineState state = GoroutineState.current();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        while (chain.size() > mark) {
            DeferredCall dc = chain.pop();
            try {
                dc.invoke();
            } catch (PanicException p) {
                // A panic during Goexit unwind is processed as a panic
                // unwind (gc): if recovered, the goexit completes; if not,
                // the panic wins.
                PanicException rest = unwindPanicking(mark, p);
                if (rest != null) {
                    throw rest;
                }
                // Recovered: the goexit continues (remaining defers run).
            }
        }
    }
}
