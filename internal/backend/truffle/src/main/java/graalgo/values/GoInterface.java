package graalgo.values;

/**
 * A Go interface value: {@code (descriptor, data)} plus the itab (D5).
 *
 * <p>{@code descriptor} identifies the dynamic type (null for the nil
 * interface value); {@code data} is the dynamic value in its D5 Java
 * carrier; {@code itab} is the interface method table — an opaque,
 * <em>immutable</em> object (D6: itab speculation needs no Assumptions).
 * Interface dispatch (PICs, limit ~3, hash fallback — D6) arrives with
 * WS D; the polymorphism probes ({@link InterfaceDispatchProbes}) are the
 * M3 hook for the M9 measurement.
 */
public final class GoInterface {
    /** The nil interface value: comparisons against nil use identity. */
    public static final GoInterface NIL = new GoInterface(null, null, null);

    /**
     * P3-M7: normalizes a value of static interface type to a {@code GoInterface}.
     * A nil Go interface value may flow as Java {@code null} (e.g. a nil
     * {@code error} parameter or a nil interface switch tag — cf.
     * {@code TypeSwitchNode}'s explicit null handling); normalize to
     * {@link #NIL} so callers take the nil-interface paths instead of
     * NPEing. A typed nil is never Java null (it carries a descriptor),
     * so the normalization is exact.
     */
    public static GoInterface narrow(Object v) {
        return v == null ? NIL : (GoInterface) v;
    }

    private final Object descriptor;
    private final Object data;
    private final Object itab;

    public GoInterface(Object descriptor, Object data, Object itab) {
        if (descriptor == null && data != null) {
            throw new IllegalArgumentException("data without a descriptor is not a valid interface value");
        }
        // (non-null descriptor, null data) is a typed nil (e.g. a nil
        // *Point in an interface); (null, null) is the nil interface.
        this.descriptor = descriptor;
        this.data = data;
        this.itab = itab;
    }

    public boolean isNil() {
        return descriptor == null;
    }

    public Object descriptor() {
        return descriptor;
    }

    public Object data() {
        return data;
    }

    public Object itab() {
        return itab;
    }

    @Override
    public String toString() {
        return isNil() ? "GoInterface{nil}" : "GoInterface{" + descriptor + "}";
    }
}
