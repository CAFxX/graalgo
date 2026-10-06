package graalgo.iface;

import graalgo.values.GoFunc;

/**
 * One installed interface method (P3-M3, WS D): the {@link GoFunc} to
 * invoke plus the receiver adaptation path.
 *
 * <p>{@code recvPath} is the exporter's wire {@code "index"} field: the
 * go/types embedding path from the dynamic type to the method's declaring
 * receiver — empty for direct methods, e.g. {@code [0]} for a method
 * promoted via embedded field 0. Dispatch walks it
 * ({@code SelectorNode.readFieldPath}) so a promoted method receives the
 * embedded value, not the outer struct.
 */
public record IfaceMethodTarget(GoFunc func, int[] recvPath) {
    public IfaceMethodTarget {
        recvPath = recvPath == null ? new int[0] : recvPath.clone();
    }

    /** True for directly-declared methods (no receiver adaptation). */
    public boolean isDirect() {
        return recvPath.length == 0;
    }
}
