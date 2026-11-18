package graalgo.runtime;

/**
 * Goroutine-local execution state: the defer chain, the in-flight panic,
 * the recover-allowed flag, and the Go call stack (M5, WS F; SPEC §3.5).
 *
 * <p>The lookup is virtual-thread-scoped via {@link ThreadLocal}
 * (SPEC §3.5:432–433). Signatures are the frozen ones (freeze §3.5);
 * {@code FreezeSignatureTest} checks them.
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
    // Holder. Two views of one per-thread value, written together by
    // install/restore on the owning thread:
    //  - HOLDER, a host ThreadLocal: the lookup for runtime code (behind
    //    @TruffleBoundary) and for runs without a polyglot context (unit
    //    tests, the graalgo-run/SchedMain launchers);
    //  - inside a Go context, the thread's GoThreadSlot (the language's
    //    ContextThreadLocal, truffle-design §2.5), which compiled code of a
    //    language-bound root reads inline: current(RootNode).
    // ------------------------------------------------------------------

    private static final ThreadLocal<GoroutineState> HOLDER = new ThreadLocal<>();

    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static GoroutineState current() {
        return HOLDER.get();
    }

    /**
     * The current goroutine's state, as seen from code of {@code root}. A
     * root built inside a Go context (it has the language) only ever runs on
     * threads entered in that context, where the thread slot is
     * authoritative: partial evaluation folds the language check and reads
     * the context thread-local inline, with no boundary call. Language-less
     * roots (built without a context) fall back to {@link #current()}.
     */
    public static GoroutineState current(com.oracle.truffle.api.nodes.RootNode root) {
        graalgo.language.GoLanguage lang = root.getLanguage(graalgo.language.GoLanguage.class);
        if (lang != null) {
            return lang.threadSlot().state;
        }
        return current();
    }

    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static GoroutineState install(GoroutineState next) {
        GoroutineState prev = HOLDER.get();
        HOLDER.set(next);
        setSlot(next);
        return prev;
    }

    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void restore(GoroutineState prev) {
        HOLDER.set(prev);
        setSlot(prev);
    }

    private static void setSlot(GoroutineState s) {
        graalgo.language.GoContext c = graalgo.language.GoContext.activeOrNull();
        if (c != null && c.isEntered()) {
            c.threadSlot().state = s;
        }
    }
}
