package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.types.Descriptors;
import graalgo.go.types.GoType;
import graalgo.go.values.GoMap;
import graalgo.go.values.Iface;
import graalgo.runtime.GoChan;

/**
 * What gc's runtime provides to {@code internal/reflectlite} by {@code //go:linkname}: its
 * seven functions without a body (toolexec-design §5.6, W9a); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. reflectlite itself is upstream's Go code,
 * reading descriptor images ({@link Descriptors}) through the pointer model.
 *
 * <p>A {@code *abi.Type} argument is a descriptor image; {@link Descriptors#typeOf} maps it back
 * to the type whose layout the operation then uses.
 */
final class ReflectliteIntrinsics {
    private ReflectliteIntrinsics() {}

    private static GoType type(Node node, Object rtype) {
        GoType t = Descriptors.typeOf(rtype);
        if (t == null) {
            // A descriptor this runtime did not build (reflect's StructOf and friends, W9b).
            throw Panics.fatal(node, "type descriptor not built by the runtime");
        }
        return t;
    }

    /** {@code resolveNameOff(ptrInModule unsafe.Pointer, off int32) unsafe.Pointer}. */
    static Object internal_reflectlite_resolveNameOff(Node node, Object ptrInModule, long off) {
        return GoProcess.get(node).types.descriptors.resolve(off);
    }

    /** {@code resolveTypeOff(rtype unsafe.Pointer, off int32) unsafe.Pointer}. */
    static Object internal_reflectlite_resolveTypeOff(Node node, Object rtype, long off) {
        return GoProcess.get(node).types.descriptors.resolve(off);
    }

    /** {@code unsafe_New(*abi.Type) unsafe.Pointer}: a zeroed variable of the type. */
    @TruffleBoundary
    static Object internal_reflectlite_unsafe_New(Node node, Object rtype) {
        return type(node, rtype).zeroFlat();
    }

    /** {@code typedmemmove(t *abi.Type, dst, src unsafe.Pointer)}: {@code *dst = *src} for a value of type t. */
    @TruffleBoundary
    static void internal_reflectlite_typedmemmove(Node node, Object rtype, Object dst, Object src) {
        GoType t = type(node, rtype);
        if (t.gcSize() == 0 || Unsafe.sameLocation(dst, src)) {
            return;
        }
        if (dst == null || src == null) {
            throw Panics.nilDeref(node);
        }
        Unsafe.storeThrough(node, dst, t, Unsafe.loadThrough(node, src, t));
    }

    /**
     * {@code ifaceE2I(t *abi.Type, src any, dst unsafe.Pointer)}: {@code *dst = src.(T)} for the
     * interface type T that t describes (gc's assertE2I: the conversion panic when the dynamic
     * type lacks a method).
     */
    @TruffleBoundary
    static void internal_reflectlite_ifaceE2I(Node node, Object rtype, Object src, Object dst) {
        GoType t = type(node, rtype);
        Iface i = (Iface) src;
        GoType any = GoProcess.get(node).types.emptyInterface;
        if (i == null || i.type.itab(t) == null) {
            throw Panics.typeAssert(node, any, i == null ? null : i.type, t);
        }
        Unsafe.storeThrough(node, dst, t, i);
    }

    /** {@code chanlen(ch unsafe.Pointer) int}. */
    @TruffleBoundary
    static long internal_reflectlite_chanlen(Node node, Object ch) {
        return ch == null ? 0 : ((GoChan) ch).buffered();
    }

    /** {@code maplen(m unsafe.Pointer) int}. */
    @TruffleBoundary
    static long internal_reflectlite_maplen(Node node, Object m) {
        return m == null ? 0 : ((GoMap) m).len();
    }
}
