package graalgo.iface;

import graalgo.values.GoFunc;

/**
 * One installed interface method (P3-M3, WS D): the {@link GoFunc} to
 * invoke plus the receiver adaptation data.
 *
 * <p>{@code recvPath} is the exporter's wire {@code "index"} field: the
 * go/types embedding path from the dynamic type to the method's declaring
 * receiver — empty for direct methods, e.g. {@code [0]} for a method
 * promoted via embedded field 0. Dispatch walks it
 * ({@code SelectorNode.readFieldPath}) so a promoted method receives the
 * embedded value, not the outer struct.
 *
 * <p>{@code pointerReceiver} is the wire {@code "recv"} field: true for
 * pointer-receiver methods. A value-receiver method promoted via a
 * pointer-embedded field gets one trailing dereference (gc's implicit
 * {@code (*p)}); a pointer-receiver method promoted via a value-embedded
 * struct field takes the address of the promoted field value (wire
 * {@code "addrOf"} true, P3-M9 — gc's implicit {@code &}). The remaining
 * combination (pointer receiver via a value-embedded non-struct field)
 * is marked unsupported (wire {@code "index"} null) and fails fast with
 * a named P3-M3 scope error at dispatch — only when that method is
 * actually dispatched (R2 P2-7: narrowing; the itab build carries the
 * unsupported target so supported methods keep working).
 */
public record IfaceMethodTarget(GoFunc func, int[] recvPath, boolean pointerReceiver, boolean addrOf) {
    public IfaceMethodTarget {
        // A null path marks an unsupported promotion (wire "index": null);
        // it must survive (not normalize to empty) so the itab build can
        // fail fast with the named scope error.
        recvPath = recvPath == null ? null : recvPath.clone();
    }

    /** False for unsupported promotions (wire {@code "index"} null). */
    public boolean supported() {
        return recvPath != null;
    }

    /** True for directly-declared methods (no receiver adaptation). */
    public boolean isDirect() {
        return recvPath != null && recvPath.length == 0;
    }
}
