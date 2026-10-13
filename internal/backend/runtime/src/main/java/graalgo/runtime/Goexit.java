package graalgo.runtime;

import com.oracle.truffle.api.nodes.ControlFlowException;

/**
 * Go's {@code runtime.Goexit} as a Truffle control-flow exception (M5, WS F;
 * SPEC §3.5, GG-0019).
 *
 * <p>Per D3 [DECIDED by SPEC], Goexit is a {@link ControlFlowException} (no
 * trace needed): it unwinds through Truffle frames, running deferred calls
 * via the {@link DeferChain}, but is not recoverable ({@link Panic#recover()}
 * returns nil during a Goexit unwind) and never produces crash output.
 *
 * <p>In Phase 1 the only goroutine is {@code main}, so a Goexit always ends
 * in gc's {@code fatal error: no goroutines (main called runtime.Goexit)}
 * (exit 2) — the launcher reports it. The exit-0 termination applies to
 * non-main goroutines (Phase 2).
 */
public final class Goexit extends ControlFlowException {
    private Goexit() {}

    /** Throws the Goexit control-flow exception (frozen). */
    public static void goexit() {
        throw new Goexit();
    }
}
