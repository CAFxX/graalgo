package graalgo.runtime;

import com.oracle.truffle.api.exception.AbstractTruffleException;
import com.oracle.truffle.api.nodes.Node;

/**
 * Go's {@code panic} as a Truffle exception (M5, WS F; SPEC §3.5, GG-0019).
 *
 * <p>Per D3 [DECIDED by SPEC], panic is an {@link AbstractTruffleException}
 * (lazy traces — recovered panics stay cheap; the
 * {@code ControlFlowException} alternative was rejected because fatal panics
 * need printed traces anyway). It carries the panicked Go value in its D5
 * Java mapping, plus the dynamic-type descriptor for {@code recover()}
 * boxing and {@code printpanicval}-compatible rendering.
 *
 * <p>The Go call stack is captured as a persistent {@link GoStackFrame} head
 * in O(1) at throw time; the textual trace is rendered only if the panic
 * goes uncaught (lazy, per SPEC).
 */
public final class PanicException extends AbstractTruffleException {
    private final Object goValue;
    private final Object descriptor;
    private final String goTypeName;
    private final GoStackFrame stackTop;
    private final Node throwSite;
    private boolean recovered;

    /**
     * @param goValue    the panicked Go value (D5 mapping; never null —
     *                   {@link Panic#throwPanic} converts nil).
     * @param descriptor the dynamic-type descriptor (a
     *                   {@code graalgo.values.GoTypeDescriptor}), or null on
     *                   the degraded path (recover returns the raw value).
     * @param goTypeName Go-syntax type name for {@code (type) 0xADDR}
     *                   rendering (e.g. {@code []int}); null degrades to the
     *                   descriptor name.
     * @param throwSite  the throwing AST node (trace line attribution), or
     *                   null.
     */
    public PanicException(Object goValue, Object descriptor, String goTypeName, Node throwSite) {
        this.goValue = goValue;
        this.descriptor = descriptor;
        this.goTypeName = goTypeName;
        this.throwSite = throwSite;
        GoroutineState s = GoroutineState.current();
        this.stackTop = s == null ? new GoStackFrame() : s.goStack();
    }

    /** The panicked Go value (D5 mapping). */
    public Object goValue() {
        return goValue;
    }

    /** The dynamic-type descriptor, or null on the degraded path. */
    public Object descriptor() {
        return descriptor;
    }

    /** Go-syntax type name for composite rendering, or null. */
    public String goTypeName() {
        return goTypeName;
    }

    /** The throwing AST node, or null. */
    public Node throwSite() {
        return throwSite;
    }

    /** The Go call stack captured at throw time (innermost first). */
    public GoStackFrame stackTop() {
        return stackTop;
    }

    /** Marks the panic as stopped by {@code recover()}. */
    public void markRecovered() {
        recovered = true;
    }

    /** Whether {@code recover()} stopped this panic. */
    public boolean isRecovered() {
        return recovered;
    }
}
