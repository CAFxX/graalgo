package graalgo.runtime;

/**
 * A Go error value carried by a panic (M5, WS F).
 *
 * <p>Models what gc's {@code preprintpanics} leaves behind: a value whose
 * {@code Error()} text is printed. The text is stored <em>with</em> any
 * {@code "runtime error: "} prefix, exactly as gc's {@code Error()}
 * implementations produce it (e.g. {@code boundsError.Error()} in
 * runtime/error.go prepends the prefix itself).
 *
 * <p>P0-fmt-016: a recovered runtime panic flows through fmt as a raw
 * {@code GoErrorValue} (not a {@code GoInterface}), so the value also
 * carries gc's panic-type identity for faithful {@code %T}/{@code %v}/
 * {@code %#v}/{@code %p} rendering:
 * <ul>
 *   <li>{@code typeName}: gc's {@code %T} name, e.g.
 *       {@code "runtime.boundsError"}, {@code "runtime.errorString"},
 *       {@code "runtime.plainError"}, {@code "*runtime.PanicNilError"}.
 *       In gc these are the actual runtime types ({@code errorString} and
 *       {@code plainError} are string kinds; {@code boundsError} is a
 *       struct).</li>
 *   <li>{@code goSyntax}: the pre-rendered {@code %#v} (Go-syntax) form,
 *       e.g. {@code runtime.boundsError{x:5, y:2, signed:false, code:0x0}}
 *       or the Go-quoted underlying string for string-kind errors. Null
 *       when the form is not deterministically renderable (e.g.
 *       {@code TypeAssertionError}, whose gc form embeds addresses).</li>
 *   <li>{@code structForm}: the structural {@code %v} form printed when
 *       method dispatch is disabled (inside {@code badVerb}, mirroring
 *       gc's {@code p.erroring}): {@code {5 2 false 0}} for bounds errors,
 *       the raw underlying string for string-kind errors. Null when
 *       unknown.</li>
 * </ul>
 */
public class GoErrorValue {
    private final String errorString;
    private final boolean signalSegv;
    private final String typeName;
    private final String goSyntax;
    private final String structForm;

    public GoErrorValue(String errorString) {
        this(errorString, false, null, null, null);
    }

    public GoErrorValue(String errorString, boolean signalSegv) {
        this(errorString, signalSegv, null, null, null);
    }

    /**
     * Full constructor with gc panic-type identity (P0-fmt-016).
     *
     * @param errorString the full {@code Error()} text (with any
     *                    {@code "runtime error: "} prefix gc includes)
     * @param signalSegv  true for nil-pointer dereferences
     * @param typeName    gc's {@code %T} name, or null if unknown
     * @param goSyntax    pre-rendered {@code %#v} form, or null
     * @param structForm  structural {@code %v} form for badVerb, or null
     */
    public GoErrorValue(String errorString, boolean signalSegv,
                        String typeName, String goSyntax, String structForm) {
        if (errorString == null) {
            throw new IllegalArgumentException("errorString must not be null");
        }
        this.errorString = errorString;
        this.signalSegv = signalSegv;
        this.typeName = typeName;
        this.goSyntax = goSyntax;
        this.structForm = structForm;
    }

    /** The {@code Error()} text, as gc's preprintpanics would substitute it. */
    public String errorString() {
        return errorString;
    }

    /** Whether the crash prints the {@code [signal SIGSEGV: ...]} line. */
    public boolean signalSegv() {
        return signalSegv;
    }

    /** gc's {@code %T} name for this panic value, or null if unknown. */
    public String typeName() {
        return typeName;
    }

    /** Pre-rendered {@code %#v} (Go-syntax) form, or null. */
    public String goSyntax() {
        return goSyntax;
    }

    /** Structural {@code %v} form for badVerb inner rendering, or null. */
    public String structForm() {
        return structForm;
    }

    @Override
    public String toString() {
        return "GoErrorValue{" + errorString + "}";
    }
}
