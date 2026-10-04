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
