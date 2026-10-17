package graalgo.values;

/**
 * Referent of a {@code *abi.EmptyInterface} pointer produced by the
 * {@code *(*abi.EmptyInterface)(unsafe.Pointer(&x))} idiom (P3-M10, WS D).
 *
 * <p>In gc this reinterprets the interface value's memory as
 * {@code struct { Type *Type; Data unsafe.Pointer }}. D5's interface is
 * the opaque {@link GoInterface}, so the unsafe layer synthesizes this
 * view instead: field 0 ({@code Type}) is the {@code *abi.Type} for the
 * interface's dynamic type, field 1 ({@code Data}) is the data word as an
 * {@code unsafe.Pointer} (0 for nil data).
 *
 * <p>Immutable and safe to share.
 */
public final class EfaceRef {
    private final AbiTypeRef typeRef;
    private final long dataHandle;

    public EfaceRef(AbiTypeRef typeRef, long dataHandle) {
        this.typeRef = typeRef;
        this.dataHandle = dataHandle;
    }

    /**
     * Field read by go/types field index: 0 = {@code Type} (a
     * {@code *abi.Type}, i.e. a {@link graalgo.values.GoPointer} to an
     * {@link AbiTypeRef}), 1 = {@code Data} (unsafe.Pointer handle).
     */
    public Object field(int idx) {
        return switch (idx) {
            case 0 -> new GoPointer(typeRef);
            case 1 -> Long.valueOf(dataHandle);
            default -> throw new IllegalStateException(
                    "EfaceRef: no field " + idx + " (EmptyInterface has Type, Data)");
        };
    }

    @Override
    public String toString() {
        return "EfaceRef{" + typeRef + ", data=" + dataHandle + "}";
    }
}
