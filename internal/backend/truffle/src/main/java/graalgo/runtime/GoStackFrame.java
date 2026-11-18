package graalgo.runtime;

import com.oracle.truffle.api.source.SourceSection;

/**
 * One Go call-stack frame, for panic traces (M5, WS F).
 *
 * <p>Frames form a persistent (immutable) linked list hanging off
 * {@link GoroutineState}: pushing returns a new head, so a
 * {@link PanicException} can capture the stack in O(1) at throw time and
 * render it lazily only if the panic goes uncaught (SPEC §3.5: panic traces
 * are lazy; recovered panics stay cheap).
 */
public final class GoStackFrame {
    private final GoStackFrame parent;
    private final String funcName;
    /**
     * The source section for this frame (P3-M4): updated to the call site
     * when this function calls another (gc's trace shows the call site
     * in each caller frame, not the function definition).
     */
    private SourceSection section;
    /**
     * The section at push time (the function definition site). Unlike
     * {@link #section}, never mutated by call-site updates — used for
     * {@code runtime.Func.Entry} attribution.
     */
    private final SourceSection defSection;

    public GoStackFrame(GoStackFrame parent, String funcName, SourceSection section) {
        this.parent = parent;
        this.funcName = funcName;
        this.section = section;
        this.defSection = section;
    }

    /**
     * Updates the frame's section to the call site (P3-M4, WS C):
     * called by the call node before invoking the callee, so a panic
     * traceback shows where this function called, like gc.
     */
    public void setSection(SourceSection section) {
        this.section = section;
    }

    /** The empty stack. */
    public GoStackFrame() {
        this(null, "", null);
    }

    public GoStackFrame parent() {
        return parent;
    }

    /** Go-qualified function name, e.g. {@code main.main} or {@code main.main.func1}. */
    public String funcName() {
        return funcName;
    }

    /**
     * The source section to attribute this frame in a trace: the call-site
     * section for caller frames. The panicking (innermost) frame's line is
     * overridden with the throw site when the trace is rendered.
     */
    public SourceSection section() {
        return section;
    }

    /** The function definition site (null for the empty stack). */
    public SourceSection defSection() {
        return defSection;
    }

    /** True for the empty stack. */
    public boolean isEmpty() {
        return parent == null;
    }
}
