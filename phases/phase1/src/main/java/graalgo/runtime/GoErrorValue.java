package graalgo.runtime;

/**
 * A Go error value carried by a panic (M5, WS F).
 *
 * <p>Models what gc's {@code preprintpanics} leaves behind: a value whose
 * {@code Error()} text is printed. The text is stored <em>with</em> any
 * {@code "runtime error: "} prefix, exactly as gc's {@code Error()}
 * implementations produce it (e.g. {@code boundsError.Error()} in
 * runtime/error.go prepends the prefix itself).
 */
public class GoErrorValue {
    private final String errorString;
    private final boolean signalSegv;

    /**
     * @param errorString the full {@code Error()} text (with any
     *                    {@code "runtime error: "} prefix gc includes).
     * @param signalSegv  true for nil-pointer dereferences: the crash prints
     *                    the {@code [signal SIGSEGV: ...]} line after the
     *                    panic header, exactly like gc.
     */
    public GoErrorValue(String errorString, boolean signalSegv) {
        if (errorString == null) {
            throw new IllegalArgumentException("errorString must not be null");
        }
        this.errorString = errorString;
        this.signalSegv = signalSegv;
    }

    public GoErrorValue(String errorString) {
        this(errorString, false);
    }

    /** The {@code Error()} text, as gc's preprintpanics would substitute it. */
    public String errorString() {
        return errorString;
    }

    /** Whether the crash prints the {@code [signal SIGSEGV: ...]} line. */
    public boolean signalSegv() {
        return signalSegv;
    }

    @Override
    public String toString() {
        return "GoErrorValue{" + errorString + "}";
    }
}
