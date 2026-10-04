package graalgo.runtime;

/**
 * gc-exact runtime panic values (M5, WS F).
 *
 * <p>Ports the panic constructors from go1.27.1's runtime/panic.go and the
 * {@code boundsError} formats from runtime/error.go, so every converted
 * {@code GoPanic} site (index, slice, nil-map write, nil deref, divide by
 * zero, negative shift, interface conversion) produces the exact gc value.
 *
 * <p>Bounds checks mirror gc's ssagen check order (see {@code slice()} in
 * cmd/compile/internal/ssagen/ssa.go): the high/max bound is checked
 * against length/capacity before the low bound is checked against the high
 * bound. The {@link BoundsCode} formats are verbatim from
 * {@code boundsErrorFmts}/{@code boundsNegErrorFmts}.
 */
public final class GoErrors {
    private GoErrors() {}

    /** Mirrors abi.BoundsKind; formats verbatim from runtime/error.go. */
    public enum BoundsCode {
        INDEX("index out of range [%x] with length %y", "index out of range [%x]"),
        SLICE_ALEN("slice bounds out of range [:%x] with length %y", "slice bounds out of range [:%x]"),
        SLICE_ACAP("slice bounds out of range [:%x] with capacity %y", "slice bounds out of range [:%x]"),
        SLICE_B("slice bounds out of range [%x:%y]", "slice bounds out of range [%x:]"),
        SLICE3_ALEN("slice bounds out of range [::%x] with length %y", "slice bounds out of range [::%x]"),
        SLICE3_ACAP("slice bounds out of range [::%x] with capacity %y", "slice bounds out of range [::%x]"),
        SLICE3_B("slice bounds out of range [:%x:%y]", "slice bounds out of range [:%x:]"),
        SLICE3_C("slice bounds out of range [%x:%y:]", "slice bounds out of range [%x::]");

        final String fmt;
        final String negFmt;

        BoundsCode(String fmt, String negFmt) {
            this.fmt = fmt;
            this.negFmt = negFmt;
        }
    }

    /**
     * Renders a {@code boundsError}: {@code x} is rendered signed or
     * unsigned per {@code signed}; a negative signed {@code x} selects the
     * overriding negative format (which omits {@code y}); {@code y} always
     * renders signed (it is a length/capacity).
     */
    public static GoErrorValue bounds(BoundsCode code, long x, long y, boolean signed) {
        String xs = signed ? Long.toString(x) : Long.toUnsignedString(x);
        String msg;
        if (signed && x < 0) {
            msg = code.negFmt.replace("%x", xs);
        } else {
            msg = code.fmt.replace("%x", xs).replace("%y", Long.toString(y));
        }
        return new GoErrorValue("runtime error: " + msg);
    }

    /** Index out of range (signed or unsigned index). */
    public static GoErrorValue indexOutOfRange(long idx, long len, boolean signed) {
        return bounds(BoundsCode.INDEX, idx, len, signed);
    }

    /** Assignment to an entry in a nil map. */
    public static GoErrorValue assignmentToNilMap() {
        return new GoErrorValue("assignment to entry in nil map");
    }

    /** Nil pointer/map/... dereference (adds the SIGSEGV traceback line). */
    public static GoErrorValue nilDereference() {
        return new GoErrorValue(
                "runtime error: invalid memory address or nil pointer dereference", true);
    }

    /** Integer divide by zero. */
    public static GoErrorValue integerDivideByZero() {
        return new GoErrorValue("runtime error: integer divide by zero");
    }

    /** Negative shift amount. */
    public static GoErrorValue negativeShift() {
        return new GoErrorValue("runtime error: negative shift amount");
    }

    /**
     * Failed type assertion: {@code interface conversion: <iface> is
     * <dynamic>, not <asserted>} (gc renders all three in Go syntax).
     */
    public static GoErrorValue interfaceConversion(String iface, String dynamic, String asserted) {
        return new GoErrorValue("interface conversion: " + iface + " is " + dynamic + ", not " + asserted);
    }
}
