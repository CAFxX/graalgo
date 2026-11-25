package graalgo.frontend;

/**
 * {@code break} as a control-flow signal (M4, WS E). Caught by the
 * innermost enclosing {@code for}/{@code range}/{@code switch} (unlabeled),
 * or by the construct carrying the label. Never escapes a loop or switch
 * node — it is not an error.
 */
public final class BreakSignal extends RuntimeException {
    private final String label;

    public BreakSignal(String label) {
        super(null, null, false, false);
        this.label = label;
    }

    /**
     * True when this signal targets the construct carrying
     * {@code constructLabel}: an unlabeled signal always targets the
     * innermost enclosing loop/switch (inner constructs would have caught
     * it first); a labeled signal targets only its label.
     */
    public boolean matches(String constructLabel) {
        return label == null || label.equals(constructLabel);
    }

    public String label() {
        return label;
    }
}
