package graalgo.iface;

import java.util.List;

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
 * field takes the address of the promoted field value (wire
 * {@code "addrOf"} true — gc's implicit {@code &}). Struct-typed fields
 * are read by reference (P3-M9); any other field type (named scalars
 * etc.) addresses the live field through a writeback field cell
 * (review3-traps P1-I4), exactly like {@code &s.f}. A null path (wire
 * {@code "index"} null) still marks an unsupported promotion and fails
 * fast with a Go panic at dispatch — only when that method is actually
 * dispatched (R2 P2-7: narrowing; the itab build carries the unsupported
 * target so supported methods keep working). The exporter no longer emits
 * the null mark; it survives as defense-in-depth.
 *
 * <p>{@code embeddedIfaceId} is the wire type-table id of the embedded
 * interface's {@code interface}-kind entry for a method promoted via an
 * embedded interface field (P3-M9, e.g. {@code (*crypto/tls.listener).Addr}
 * from embedded {@code net.Listener}); -1 for ordinary entries. Such a
 * redispatch entry carries no {@link GoFunc} (func is null): the
 * dispatcher reads {@code recvPath} to the interface field value and
 * dispatches the method on it, using {@code embeddedIfaceMethods} (the
 * embedded interface's method names in declaration order) for the inner
 * itab.
 */
public record IfaceMethodTarget(GoFunc func, int[] recvPath, boolean pointerReceiver, boolean addrOf,
        int embeddedIfaceId, List<String> embeddedIfaceMethods) {
    public IfaceMethodTarget {
        // A null path marks an unsupported promotion (wire "index": null);
        // it must survive (not normalize to empty) so the itab build can
        // fail fast with the named scope error.
        recvPath = recvPath == null ? null : recvPath.clone();
        embeddedIfaceMethods = embeddedIfaceMethods == null ? List.of()
                : List.copyOf(embeddedIfaceMethods);
    }

    /** False for unsupported promotions (wire {@code "index"} null). */
    public boolean supported() {
        return recvPath != null;
    }

    /** True for directly-declared methods (no receiver adaptation). */
    public boolean isDirect() {
        return recvPath != null && recvPath.length == 0;
    }

    /** True for embedded-interface redispatch entries (no GoFunc). */
    public boolean isEmbeddedRedispatch() {
        return embeddedIfaceId >= 0;
    }
}
