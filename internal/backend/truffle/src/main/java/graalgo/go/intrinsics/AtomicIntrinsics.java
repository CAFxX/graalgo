package graalgo.go.intrinsics;

import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.Panics;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;
import java.lang.invoke.MethodHandles;
import java.lang.invoke.VarHandle;

/**
 * Intrinsics of package {@code sync/atomic}: upstream declares these functions without bodies
 * (assembly, and for the pointer forms the runtime by {@code //go:linkname}); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. The typed wrappers ({@code atomic.Int64},
 * {@code atomic.Pointer[T]}, ...) are upstream's Go code on top of them.
 *
 * <p>The addressed word is a slot of the referent's scalar or reference array (SPEC §3.4,
 * GG-0061/GG-0062): a pointer is the {@link Flat} itself or a {@link Ptr} with slot offsets, and
 * the operation is a {@link VarHandle} access to that array element. Integers are stored in
 * canonical extended form (truffle-design §15), so the 32-bit additions re-normalize in a
 * compare-and-set loop; every other operation preserves the form.
 */
final class AtomicIntrinsics {
    private AtomicIntrinsics() {}

    private static final VarHandle LONGS = MethodHandles.arrayElementVarHandle(long[].class);
    private static final VarHandle REFS = MethodHandles.arrayElementVarHandle(Object[].class);

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
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapInt32(Node node, Object addr, long old, long v) {
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddInt32(Node node, Object addr, long delta) {
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
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrInt32(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadInt32(Node node, Object addr) {
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreInt32(Node node, Object addr, long v) {
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapUint32(Node node, Object addr, long v) {
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapUint32(Node node, Object addr, long old, long v) {
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddUint32(Node node, Object addr, long delta) {
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
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrUint32(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadUint32(Node node, Object addr) {
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreUint32(Node node, Object addr, long v) {
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapInt64(Node node, Object addr, long v) {
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapInt64(Node node, Object addr, long old, long v) {
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddInt64(Node node, Object addr, long delta) {
        return (long) LONGS.getAndAdd(prims(node, addr), index(addr), delta) + delta;
    }

    static long sync_atomic_AndInt64(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrInt64(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadInt64(Node node, Object addr) {
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreInt64(Node node, Object addr, long v) {
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapUint64(Node node, Object addr, long v) {
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapUint64(Node node, Object addr, long old, long v) {
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddUint64(Node node, Object addr, long delta) {
        return (long) LONGS.getAndAdd(prims(node, addr), index(addr), delta) + delta;
    }

    static long sync_atomic_AndUint64(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrUint64(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadUint64(Node node, Object addr) {
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreUint64(Node node, Object addr, long v) {
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    static long sync_atomic_SwapUintptr(Node node, Object addr, long v) {
        return (long) LONGS.getAndSet(prims(node, addr), index(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapUintptr(Node node, Object addr, long old, long v) {
        return LONGS.compareAndSet(prims(node, addr), index(addr), old, v);
    }

    static long sync_atomic_AddUintptr(Node node, Object addr, long delta) {
        return (long) LONGS.getAndAdd(prims(node, addr), index(addr), delta) + delta;
    }

    static long sync_atomic_AndUintptr(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseAnd(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_OrUintptr(Node node, Object addr, long mask) {
        return (long) LONGS.getAndBitwiseOr(prims(node, addr), index(addr), mask);
    }

    static long sync_atomic_LoadUintptr(Node node, Object addr) {
        return (long) LONGS.getVolatile(prims(node, addr), index(addr));
    }

    static void sync_atomic_StoreUintptr(Node node, Object addr, long v) {
        LONGS.setVolatile(prims(node, addr), index(addr), v);
    }

    // Pointers live in reference slots. Go pointer equality is location equality
    // (Ptr.same), not Java identity, so compare-and-swap compares before it swaps.
    static Object sync_atomic_SwapPointer(Node node, Object addr, Object v) {
        return REFS.getAndSet(refs(node, addr), refIndex(addr), v);
    }

    static boolean sync_atomic_CompareAndSwapPointer(Node node, Object addr, Object old, Object v) {
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
        return REFS.getVolatile(refs(node, addr), refIndex(addr));
    }

    static void sync_atomic_StorePointer(Node node, Object addr, Object v) {
        REFS.setVolatile(refs(node, addr), refIndex(addr), v);
    }
}
