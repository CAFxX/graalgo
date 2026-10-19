package graalgo.frontend;

import graalgo.values.GoInterface;
import graalgo.values.GoKind;

/**
 * Interop services {@link DerefNode} needs from the interop module
 * (p3unsafe / p3reflect), discovered via {@link java.util.ServiceLoader}.
 *
 * <p>Exists to break what would otherwise be a circular module dependency:
 * the interop module compiles against the truffle+runtime modules as frozen
 * dependencies, so truffle-module code cannot reference interop classes at
 * compile time (a fresh {@code make build} compiles truffle+runtime before
 * interop exists). This is the same pattern as {@link WireKindRegistrar}:
 * the contract lives in the consuming module, the implementation in the
 * provider module, wired at runtime via {@code META-INF/services}.
 */
public interface DerefInterop {
    /**
     * Interns an {@code unsafe.Pointer} word into a handle, mirroring
     * {@code graalgo.p3unsafe.UnsafeTable.current().intern(word)}.
     *
     * @param word the pointer word (a {@code Long} address or a referent)
     * @return the interned handle
     */
    long internUnsafeWord(Object word);

    /**
     * The {@code (kind, size, align)} triple of the interface's dynamic
     * type, for materializing {@code internal/abi.Type}. Mirrors the
     * {@code graalgo.p3reflect} lookup ({@code ReflectValues.typeof} then
     * {@code ReflectTypes.table().info(tok)}); unknown tokens fail loud,
     * exactly as the direct call did.
     *
     * @param gi the interface value (non-nil)
     * @return kind/size/align of {@code gi}'s dynamic type
     */
    AbiTypeInfo abiTypeInfo(GoInterface gi);

    /**
     * The {@code (kind, size, align)} triple of a Go type, as needed for
     * {@code internal/abi.Type} materialization.
     */
    record AbiTypeInfo(GoKind kind, long size, int align) {}
}
