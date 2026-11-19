package graalgo.go.intrinsics;

import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;
import graalgo.go.values.View;
import java.lang.invoke.MethodHandles;
import java.lang.invoke.VarHandle;

/**
 * Intrinsics of package {@code sync/atomic}: upstream declares these functions without bodies
 * (assembly, and for the pointer forms the runtime by {@code //go:linkname}); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. The typed wrappers ({@code atomic.Int64},
 * {@code atomic.Pointer[T]}, ...) are upstream's Go code on top of them.
 *
 * <p>The addressed word is a slot of the referent's scalar or reference array (SPEC §3.4,
 * GG-0072/GG-0062): a pointer is the {@link Flat} itself or a {@link Ptr} with slot offsets, and
 * the operation is a {@link VarHandle} access to that array element. Integers are stored in
 * canonical extended form (truffle-design §15), so the 32-bit additions re-normalize in a
 * compare-and-set loop; every other operation preserves the form.
 *
 * <p>A word addressed through a view pointer (a conversion from {@code unsafe.Pointer} whose
 * pointee is not the referent's leaf: {@code atomic.Value}'s interface header words, an
 * {@code int32} operated on as {@code uint32}) has no slot of the named type; those operations
 * go through {@link Unsafe} under the referent's monitor.
 */
final class AtomicIntrinsics {
    private AtomicIntrinsics() {}

    private static final VarHandle LONGS = MethodHandles.arrayElementVarHandle(long[].class);
    private static final VarHandle REFS = MethodHandles.arrayElementVarHandle(Object[].class);

    private static final int V_LOAD = 0, V_STORE = 1, V_SWAP = 2, V_COMPAREANDSWAP = 3, V_ADD = 4, V_AND = 5, V_OR = 6;

    /**
     * The operation on a scalar word addressed through a view pointer (GG-0062): the word is
     * not one slot of the type the caller names, so it goes through the referent's leaf table
     * under the referent's monitor — atomic among view accesses, which is all a location that
     * needs a view gets. Results as the slot operations give them (And/Or: the old value).
     */
    private static long viewScalar(Node node, View w, int kind, int op, long a, long b) {
        GoType t = GoProcess.get(node).types.basic(kind);
        synchronized (w.obj) {
            long old = Unsafe.loadScalar(node, w.obj, w.off, t);
            switch (op) {
                case V_LOAD:
                    return old;
                case V_STORE, V_SWAP:
                    Unsafe.storeScalar(node, w.obj, w.off, t, a);
                    return old;
                case V_COMPAREANDSWAP:
                    if (old != a) {
                        return 0;
                    }
                    Unsafe.storeScalar(node, w.obj, w.off, t, b);
                    return 1;
                case V_ADD:
                    Unsafe.storeScalar(node, w.obj, w.off, t, old + a);
                    return Unsafe.loadScalar(node, w.obj, w.off, t);
                case V_AND:
                    Unsafe.storeScalar(node, w.obj, w.off, t, old & a);
                    return old;
                default:
                    Unsafe.storeScalar(node, w.obj, w.off, t, old | a);
                    return old;
            }
        }
    }

    /** As {@link #viewScalar} for a pointer word: a reference leaf or a word of a header (atomic.Value). */
    private static Object viewPointer(Node node, View w, int op, Object a, Object b) {
        GoType t = GoProcess.get(node).types.basic(GoType.UNSAFE_POINTER);
        synchronized (w.obj) {
            Object old = Unsafe.loadRef(node, w.obj, w.off, t);
            switch (op) {
                case V_LOAD:
                    return old;
                case V_STORE, V_SWAP:
                    Unsafe.storeRef(node, w.obj, w.off, t, a);
                    return old;
                default:
                    if (!Ptr.same(old, a)) {
                        return Boolean.FALSE;
                    }
                    Unsafe.storeRef(node, w.obj, w.off, t, b);
                    return Boolean.TRUE;
            }
        }
    }

    /** See the registry: nothing to pin on a JVM. */
    static long sync_atomic_runtime_procPin(Node node) {
        return 0;
    }

    static void sync_atomic_runtime_procUnpin(Node node) {
    }

    private static long[] prims(Node node, Object addr) {
        if (addr instanceof Ptr p) {
            return p.obj.prims;
        } else if (addr == null) {
            throw Panics.nilDeref(node);
        }
        return ((Flat) addr).prims;
    }

    private static int index(Object addr) {
        return addr instanceof Ptr p ? p.prim : 0;
    }

    private static Object[] refs(Node node, Object addr) {
        if (addr instanceof Ptr p) {
            return p.obj.refs;
        } else if (addr == null) {
            throw Panics.nilDeref(node);
        }
        return ((Flat) addr).refs;
    }

    private static int refIndex(Object addr) {
        return addr instanceof Ptr p ? p.ref : 0;
    }

    static long sync_atomic_SwapInt32(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT32, V_SWAP, v, 0);
        }
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapInt32(Node node, Object addr, long old, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT32, V_COMPAREANDSWAP, old, v) != 0;
        }
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddInt32(Node node, Object addr, long delta) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT32, V_ADD, delta, 0);
        }
        long[] a = prims(node, addr);
        int i = index(addr);
        for (;;) {
            long old = (long) LONGS.getVolatile(a, i);
            long v = (long) (int) (old + delta);
            if (LONGS.compareAndSet(a, i, old, v)) {
                return v;
            }
        }
    }

    static long sync_atomic_AndInt32(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT32, V_AND, mask, 0);
        }
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrInt32(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT32, V_OR, mask, 0);
        }
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadInt32(Node node, Object addr) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT32, V_LOAD, 0, 0);
        }
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreInt32(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            viewScalar(node, w, GoType.INT32, V_STORE, v, 0);
            return;
        }
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapUint32(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT32, V_SWAP, v, 0);
        }
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapUint32(Node node, Object addr, long old, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT32, V_COMPAREANDSWAP, old, v) != 0;
        }
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddUint32(Node node, Object addr, long delta) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT32, V_ADD, delta, 0);
        }
        long[] a = prims(node, addr);
        int i = index(addr);
        for (;;) {
            long old = (long) LONGS.getVolatile(a, i);
            long v = (old + delta) & 0xFFFFFFFFL;
            if (LONGS.compareAndSet(a, i, old, v)) {
                return v;
            }
        }
    }

    static long sync_atomic_AndUint32(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT32, V_AND, mask, 0);
        }
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrUint32(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT32, V_OR, mask, 0);
        }
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadUint32(Node node, Object addr) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT32, V_LOAD, 0, 0);
        }
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreUint32(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            viewScalar(node, w, GoType.UINT32, V_STORE, v, 0);
            return;
        }
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapInt64(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT64, V_SWAP, v, 0);
        }
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapInt64(Node node, Object addr, long old, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT64, V_COMPAREANDSWAP, old, v) != 0;
        }
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddInt64(Node node, Object addr, long delta) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT64, V_ADD, delta, 0);
        }
        return (long) LONGS.getAndAdd(prims(node, addr), index(addr), delta) + delta;
    }

    static long sync_atomic_AndInt64(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT64, V_AND, mask, 0);
        }
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrInt64(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT64, V_OR, mask, 0);
        }
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadInt64(Node node, Object addr) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.INT64, V_LOAD, 0, 0);
        }
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreInt64(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            viewScalar(node, w, GoType.INT64, V_STORE, v, 0);
            return;
        }
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapUint64(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT64, V_SWAP, v, 0);
        }
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapUint64(Node node, Object addr, long old, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT64, V_COMPAREANDSWAP, old, v) != 0;
        }
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddUint64(Node node, Object addr, long delta) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT64, V_ADD, delta, 0);
        }
        return (long) LONGS.getAndAdd(prims(node, addr), index(addr), delta) + delta;
    }

    static long sync_atomic_AndUint64(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT64, V_AND, mask, 0);
        }
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrUint64(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT64, V_OR, mask, 0);
        }
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadUint64(Node node, Object addr) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINT64, V_LOAD, 0, 0);
        }
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreUint64(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            viewScalar(node, w, GoType.UINT64, V_STORE, v, 0);
            return;
        }
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapUintptr(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINTPTR, V_SWAP, v, 0);
        }
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapUintptr(Node node, Object addr, long old, long v) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINTPTR, V_COMPAREANDSWAP, old, v) != 0;
        }
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddUintptr(Node node, Object addr, long delta) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINTPTR, V_ADD, delta, 0);
        }
        return (long) LONGS.getAndAdd(prims(node, addr), index(addr), delta) + delta;
    }

    static long sync_atomic_AndUintptr(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINTPTR, V_AND, mask, 0);
        }
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrUintptr(Node node, Object addr, long mask) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINTPTR, V_OR, mask, 0);
        }
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadUintptr(Node node, Object addr) {
        if (addr instanceof View w) {
            return viewScalar(node, w, GoType.UINTPTR, V_LOAD, 0, 0);
        }
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreUintptr(Node node, Object addr, long v) {
        if (addr instanceof View w) {
            viewScalar(node, w, GoType.UINTPTR, V_STORE, v, 0);
            return;
        }
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    // Pointers live in reference slots. Go pointer equality is location equality
    // (Ptr.same), not Java identity, so compare-and-swap compares before it swaps.
    static Object sync_atomic_SwapPointer(Node node, Object addr, Object v) {
        if (addr instanceof View w) {
            return viewPointer(node, w, V_SWAP, v, null);
        }
        return REFS.getAndSet(refs(node, addr), refIndex(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapPointer(Node node, Object addr, Object old, Object v) {
        if (addr instanceof View w) {
            return viewPointer(node, w, V_COMPAREANDSWAP, old, v) == Boolean.TRUE;
        }
        Object[] a = refs(node, addr);
        int i = refIndex(addr);
        for (;;) {
            Object cur = REFS.getVolatile(a, i);
            if (!Ptr.same(cur, old)) {
                return false;
            }
            if (REFS.compareAndSet(a, i, cur, v)) {
                return true;
            }
        }
    }

    static Object sync_atomic_LoadPointer(Node node, Object addr) {
        if (addr instanceof View w) {
            return viewPointer(node, w, V_LOAD, null, null);
        }
        return REFS.getVolatile(refs(node, addr), refIndex(addr));
    }

    static void sync_atomic_StorePointer(Node node, Object addr, Object v) {
        if (addr instanceof View w) {
            viewPointer(node, w, V_STORE, v, null);
            return;
        }
        REFS.setVolatile(refs(node, addr), refIndex(addr), v);
    }
}
