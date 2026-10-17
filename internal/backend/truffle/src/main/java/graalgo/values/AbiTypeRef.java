package graalgo.values;

import java.util.Map;

/**
 * Referent of a {@code *abi.Type} (or {@code *abi.PtrType}, etc.) pointer
 * value produced by unsafe reinterpretation (P3-M10, WS D).
 *
 * <p>In gc, {@code internal/abi.TypeOf} reinterprets an interface value's
 * memory as {@code abi.EmptyInterface} and threads the resulting
 * {@code *abi.Type} through {@code unsafe.Pointer}. D5 has no memory to
 * reinterpret: an interface is the opaque {@link GoInterface}, and a
 * {@code *abi.Type} is an opaque type identity. {@code AbiTypeRef} is the
 * D5 carrier for that identity: the reflect type token plus the
 * precomputed struct-field values the abi code reads through the
 * reinterpreted pointer ({@code Kind_}, {@code Elem}, ...).
 *
 * <p>The token is the canonical reflect token (the same long the
 * {@code __reflect_typeof} intrinsic returns), so
 * {@code unsafe.Pointer} of a {@code *abi.Type} yields a value the
 * {@code __reflect_t*} intrinsics accept directly. Field values are
 * precomputed at construction (in the interop layer, which owns the
 * type table); the selector path only indexes them.
 *
 * <p>Instances are immutable and safe to share.
 */
public final class AbiTypeRef {
    private final long tok;
    /**
     * Struct view this ref was created for (e.g. {@code "internal/abi.Type"},
     * {@code "internal/abi.PtrType"}); selects which field indices are valid.
     */
    private final String view;
    /** Field index → value, precomputed from the type table. */
    private final Map<Integer, Object> fields;

    public AbiTypeRef(long tok, String view, Map<Integer, Object> fields) {
        this.tok = tok;
        this.view = view;
        this.fields = Map.copyOf(fields);
    }

    /** The canonical reflect type token. */
    public long tok() {
        return tok;
    }

    /** The abi struct view (e.g. {@code "internal/abi.Type"}). */
    public String view() {
        return view;
    }

    /**
     * Struct-field read by go/types field index. Throws
     * {@link IllegalStateException} for an index the view does not define —
     * fail-loud, never a silent zero.
     */
    public Object field(int idx) {
        Object v = fields.get(idx);
        if (v == null) {
            throw new IllegalStateException(
                    "AbiTypeRef: no field " + idx + " for view " + view);
        }
        return v;
    }

    @Override
    public String toString() {
        return "AbiTypeRef{tok=" + tok + ", view=" + view + "}";
    }
}
