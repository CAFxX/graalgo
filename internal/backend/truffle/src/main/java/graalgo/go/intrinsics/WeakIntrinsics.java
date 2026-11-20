package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.Unsafe;
import graalgo.go.values.Flat;
import java.lang.ref.WeakReference;
import java.util.HashMap;
import java.util.Map;
import java.util.WeakHashMap;

/**
 * What gc's runtime provides to package {@code weak} by {@code //go:linkname} (upstream
 * runtime/mheap.go): weak pointers, here JVM weak references. Registered in
 * {@code internal/toolexec/intrinsics/registry.txt}; {@code weak} itself is upstream's source.
 */
final class WeakIntrinsics {
    private WeakIntrinsics() {}

    /** The weak handle of one location: gc's special, the {@code unsafe.Pointer} in a weak.Pointer. */
    private static final class Handle {
        final WeakReference<Object> referent;
        final long off;

        Handle(Object referent, long off) {
            this.referent = new WeakReference<>(referent);
            this.off = off;
        }
    }

    /** One handle per location, so that weak pointers to one location compare equal (as gc's). */
    private static final WeakHashMap<Object, Map<Long, Handle>> HANDLES = new WeakHashMap<>();

    /** {@code runtime_registerWeakPointer(p) unsafe.Pointer}: the handle of the location p names. */
    @TruffleBoundary
    static Object weak_runtime_registerWeakPointer(Node node, Object p) {
        if (p == null) {
            return null;
        }
        boolean flat = Unsafe.isFlatPointer(p);
        Object referent = flat ? Unsafe.referent(p) : p;
        long off = flat ? Unsafe.offset(p) : 0;
        synchronized (HANDLES) {
            return HANDLES.computeIfAbsent(referent, k -> new HashMap<>()).computeIfAbsent(off, k -> new Handle(referent, off));
        }
    }

    /** {@code runtime_makeStrongFromWeak(u) unsafe.Pointer}: the pointer, or nil once its object is gone. */
    @TruffleBoundary
    static Object weak_runtime_makeStrongFromWeak(Node node, Object u) {
        if (u == null) {
            return null;
        }
        Handle h = (Handle) u;
        Object referent = h.referent.get();
        if (referent == null) {
            return null;
        }
        return referent instanceof Flat f ? Unsafe.pointer(f, h.off) : referent;
    }
}
