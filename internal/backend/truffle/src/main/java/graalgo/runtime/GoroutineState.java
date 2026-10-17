package graalgo.runtime;

/**
 * Goroutine-local execution state: the defer chain, the in-flight panic,
 * the recover-allowed flag, and the Go call stack (M5, WS F; SPEC §3.5).
 * Same-FQN shadow (bridge B-8, inventoried in
 * {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This is a <em>forked</em> implementation of the goroutine-state holder:
 * the instance API is the real one, but the <em>lookup</em> here is a single
 * static holder (correct for a single main goroutine only). The runtime
 * module carries the same fully-qualified name
 * ({@code graalgo.runtime.GoroutineState} in
 * {@code internal/backend/runtime}) with the production lookup: a
 * {@code ThreadLocal} holder scoped to the current virtual thread
 * (SPEC §3.5:432–433).
 *
 * <p>Same-FQN overlay mechanism: the truffle module compiles this class as
 * part of its own source set (the runtime module compiles the joint round
 * first; the truffle module then compiles these same-FQN classes against
 * it). At launch, {@code internal/backend/graalgo-run} puts
 * {@code runtime/out/classes} <em>before</em> {@code truffle/out/classes} on
 * the classpath, so the runtime module's {@code ThreadLocal}-scoped copy
 * shadows this file's single-holder copy — this static-holder lookup is only
 * ever used if the launcher classpath order is violated (a launch bug), in
 * which case multi-goroutine execution would observe shared holder state.
 * The instance API is byte-identical between the two copies; only the
 * lookup differs, exactly as the frozen surface (freeze §3.5) prescribes.
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
     * there is no frame or no section.
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
    // Lookup (this copy only): a single static holder. Correct for a single
    // main goroutine only — the runtime module's same-FQN copy provides the
    // production ThreadLocal vthread-scoped holder and shadows this file at
    // launch (see the class javadoc). Every other class goes through
    // current().
    // ------------------------------------------------------------------

    private static GoroutineState current;

    /** The current goroutine's state, or null outside any Go execution. */
    public static GoroutineState current() {
        return current;
    }

    /**
     * Installs {@code next} as current, returning the previous holder for
     * {@link #restore(GoroutineState)}. Only {@code GoRootNode} calls this.
     */
    public static GoroutineState install(GoroutineState next) {
        GoroutineState prev = current;
        current = next;
        return prev;
    }

    /** Restores the holder saved by {@link #install(GoroutineState)}. */
    public static void restore(GoroutineState prev) {
        current = prev;
    }
}
