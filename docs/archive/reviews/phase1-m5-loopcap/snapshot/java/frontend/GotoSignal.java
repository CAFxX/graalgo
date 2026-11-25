package graalgo.frontend;

/**
 * {@code goto L} as a control-flow signal (M4, WS E). Caught by the
 * innermost enclosing {@link BlockNode}, which resumes execution at the
 * labeled statement (program-counter dispatch). go/types guarantees the
 * target label is reachable (no jumping into blocks or over declarations),
 * so a signal that escapes every block is an internal error.
 */
public final class GotoSignal extends RuntimeException {
    private final String label;

    public GotoSignal(String label) {
        super(null, null, false, false);
        this.label = label;
    }

    public String label() {
        return label;
    }
}
