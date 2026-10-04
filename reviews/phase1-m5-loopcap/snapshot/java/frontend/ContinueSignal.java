package graalgo.frontend;

/**
 * {@code continue} as a control-flow signal (M4, WS E). Caught by the
 * innermost enclosing loop (unlabeled), or by the loop carrying the label.
 * A {@code switch} never catches it (it propagates to the enclosing loop).
 */
public final class ContinueSignal extends RuntimeException {
    private final String label;

    public ContinueSignal(String label) {
        super(null, null, false, false);
        this.label = label;
    }

    /** Same targeting rule as {@link BreakSignal#matches(String)}. */
    public boolean matches(String constructLabel) {
        return label == null || label.equals(constructLabel);
    }

    public String label() {
        return label;
    }
}
