package graalgo.runtime;

/**
 * Goroutine-local execution state: the defer chain, the in-flight panic,
 * the recover-allowed flag, and the Go call stack (M5, WS F; SPEC §3.5).
 *
 * <p><strong>Phase-2 implementation</strong> (P2-D8): this class
 * <em>replaces</em> the Phase-1 holder of the same fully-qualified name.
 * The instance API is byte-identical; only the <em>lookup</em> changes,
 * exactly as the Phase-1 javadoc prescribed: the static holder becomes
 * virtual-thread-scoped via {@link ThreadLocal} (SPEC §3.5:432–433).
 * Signatures are unchanged (freeze §3.5); {@code FreezeSignatureTest}
 * keeps passing.
 *
 * <p>{@code ThreadLocal} is vthread-scoped because the holder lives on
 * the {@link Thread} object, and a virtual thread keeps its identity
 * across {@code LockSupport} park/unpark cycles.
 */
public final class GoroutineState {
    private final DeferChain deferChain = new DeferChain();
    private GoStackFrame goStack = new GoStackFrame();
    private PanicException panicking;
    private boolean recoverAllowed;

    public DeferChain deferChain() {
        return deferChain;
    }

    /** Pushes a Go call frame; returns the new stack head. */
    public GoStackFrame pushGoStack(String funcName, com.oracle.truffle.api.source.SourceSection section) {
        goStack = new GoStackFrame(goStack, funcName, section);
        return goStack;
    }

    /** Pops back to the given stack head (function exit). */
    public void popGoStack(GoStackFrame head) {
        goStack = head.parent();
    }

    public GoStackFrame goStack() {
        return goStack;
    }

    /**
     * Updates the innermost frame's section to the call site (P3-M4, WS
     * C): the call node invokes this before the callee runs, so a panic
     * traceback shows the call site in each caller frame (like gc),
     * rather than the caller's function definition line. No-op when
     * there is no frame or no section. (P2-D8 overlay: mirrors the
     * Phase-1 method; the instance API is byte-identical.)
     */
    public void updateTopFrameSection(com.oracle.truffle.api.source.SourceSection section) {
        if (section != null && goStack != null && !goStack.isEmpty()) {
            goStack.setSection(section);
        }
    }

    /** The in-flight panic, or null when the goroutine is not panicking. */
    public PanicException panicking() {
        return panicking;
    }

    void setPanicking(PanicException p) {
        panicking = p;
    }

    /**
     * True while directly invoking a deferred call during panic unwinding.
     * {@link Panic#recover()} returns the in-flight value only when this is
     * true: recover must be called <em>directly</em> by a deferred function
     * (gc-verified: helpers called by deferred functions see nil).
     */
    public boolean isRecoverAllowed() {
        return recoverAllowed && panicking != null;
    }

    /**
     * Sets the recover-allowed flag (M5, WS F). Called by
     * {@link DeferChain} around deferred calls and by
     * {@code graalgo.frontend.FuncValueCallNode} to clear it for nested
     * calls (recover must be called directly by the deferred function).
     */
    public void setRecoverAllowed(boolean allowed) {
        recoverAllowed = allowed;
    }

    // ------------------------------------------------------------------
    // Phase-2 holder: virtual-thread-scoped. Every other class goes
    // through current(); install/restore nest as before.
    // ------------------------------------------------------------------

    private static final ThreadLocal<GoroutineState> HOLDER = new ThreadLocal<>();

    /** The current goroutine's state, or null outside any Go execution. */
    public static GoroutineState current() {
        return HOLDER.get();
    }

    /**
     * Installs {@code next} as current, returning the previous holder for
     * {@link #restore(GoroutineState)}.
     */
    public static GoroutineState install(GoroutineState next) {
        GoroutineState prev = HOLDER.get();
        HOLDER.set(next);
        return prev;
    }

    /** Restores the holder saved by {@link #install(GoroutineState)}. */
    public static void restore(GoroutineState prev) {
        HOLDER.set(prev);
    }
}
