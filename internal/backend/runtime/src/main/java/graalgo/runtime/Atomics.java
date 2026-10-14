package graalgo.runtime;

import graalgo.runtime.sync.IntCell;
import graalgo.runtime.sync.LongCell;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.values.GoEquality;
import graalgo.values.GoPointer;

/**
 * Frozen atomic intrinsic surface (WS G, GG-0020, REAL).
 *
 * <p>Phase-1 implementations over {@link IntCell}/{@link LongCell};
 * the Phase-1 memory model is single-threaded, so these are sequentially
 * consistent by construction. Backed by {@code java.util.concurrent.atomic}
 * (no deprecated APIs).
 */
public final class Atomics {
    private Atomics() {}

    // -- int32 ----------------------------------------------------------

    /** Atomic int32 load. */
    public static int loadInt32(IntCell addr) {
        return addr.get();
    }

    /** Atomic int32 store. */
    public static void storeInt32(IntCell addr, int v) {
        addr.set(v);
    }

    /** Atomic int32 add; returns the new value. */
    public static int addInt32(IntCell addr, int delta) {
        return addr.addAndGet(delta);
    }

    /** Atomic int32 compare-and-swap; returns whether the swap happened. */
    public static boolean compareAndSwapInt32(IntCell addr, int old, int new_) {
        return addr.compareAndSet(old, new_);
    }

    /** Atomic int32 swap; returns the previous value. */
    public static int swapInt32(IntCell addr, int new_) {
        return addr.getAndSet(new_);
    }

    // -- int64 ----------------------------------------------------------

    /** Atomic int64 load. */
    public static long loadInt64(LongCell addr) {
        return addr.get();
    }

    /** Atomic int64 store. */
    public static void storeInt64(LongCell addr, long v) {
        addr.set(v);
    }

    /** Atomic int64 add; returns the new value. */
    public static long addInt64(LongCell addr, long delta) {
        return addr.addAndGet(delta);
    }

    /** Atomic int64 compare-and-swap; returns whether the swap happened. */
    public static boolean compareAndSwapInt64(LongCell addr, long old, long new_) {
        return addr.compareAndSet(old, new_);
    }

    /** Atomic int64 swap; returns the previous value. */
    public static long swapInt64(LongCell addr, long new_) {
        return addr.getAndSet(new_);
    }

    // -- pointer-sized (uintptr) -----------------------------------------

    /** Atomic uintptr load. */
    public static long loadUintptr(LongCell addr) {
        return addr.get();
    }

    /** Atomic uintptr store. */
    public static void storeUintptr(LongCell addr, long v) {
        addr.set(v);
    }

    /** Atomic uintptr compare-and-swap. */
    public static boolean compareAndSwapUintptr(LongCell addr, long old, long new_) {
        return addr.compareAndSet(old, new_);
    }

    /**
     * Atomic uintptr add (P3-M4, WS C: the {@code atomic.Uintptr} wrapper's
     * {@code Add}).
     */
    public static long addUintptr(LongCell addr, long delta) {
        return addr.addAndGet(delta);
    }

    /**
     * Atomic uintptr swap (P3-M4, WS C: the {@code atomic.Uintptr}
     * wrapper's {@code Swap}).
     */
    public static long swapUintptr(LongCell addr, long new_) {
        return addr.getAndSet(new_);
    }

    // -- bool -------------------------------------------------------------

    /**
     * Atomic bool load. Booleans ride on an {@link IntCell}
     * (0/1); Phase 2 may repack this.
     */
    public static boolean loadBool(IntCell addr) {
        return addr.get() != 0;
    }

    /** Atomic bool store. */
    public static void storeBool(IntCell addr, boolean v) {
        addr.set(v ? 1 : 0);
    }

    /** Atomic bool compare-and-swap. */
    public static boolean compareAndSwapBool(IntCell addr, boolean old, boolean new_) {
        return addr.compareAndSet(old ? 1 : 0, new_ ? 1 : 0);
    }

    /**
     * Atomic bool swap (P3-M4, WS C: the {@code atomic.Bool} wrapper's
     * {@code Swap}).
     */
    public static boolean swapBool(IntCell addr, boolean new_) {
        return addr.getAndSet(new_ ? 1 : 0) != 0;
    }

    // -- generic pointer / value (P3-M10: atomic.Pointer[T], atomic.Value) --
    // The Go value IS the AtomicReference cell (ADR-0002 D1). A Go *T is an
    // opaque Java reference (GoKind.POINTER) and a Go any is a Java object
    // (GoKind.INTERFACE); both ride on the same reference cell. A zero
    // Pointer loads Java null, which is Go's nil *T.

    /** Atomic pointer/value load. */
    public static Object loadPointer(AtomicReference<Object> addr) {
        return addr.get();
    }

    /** Atomic pointer/value store. */
    public static void storePointer(AtomicReference<Object> addr, Object v) {
        addr.set(v);
    }

    /** Atomic pointer/value swap; returns the previous value. */
    public static Object swapPointer(AtomicReference<Object> addr, Object v) {
        return addr.getAndSet(v);
    }

    /**
     * Atomic {@code Pointer[T]} compare-and-swap with Go pointer equality
     * (P3-M10): two {@code *T} values are equal when they address the same
     * storage ({@link GoPointer#sameStorageAs}), not when the wrapper
     * objects are identical — {@code &x} evaluated twice yields two
     * wrappers for one address, and gc's CAS compares addresses. A nil
     * {@code *T} (Java null, or a nil {@link GoPointer}) equals only nil,
     * mirroring the {@code POINTER} branch of {@code BinaryOpNode}.
     */
    public static boolean compareAndSwapGoPointer(AtomicReference<Object> addr, Object old, Object new_) {
        for (;;) {
            Object cur = addr.get();
            if (!goPointersEqual(cur, old)) {
                return false;
            }
            if (addr.compareAndSet(cur, new_)) {
                return true;
            }
            // Lost the race: retry against the fresh value.
        }
    }

    /** Go {@code ==} on {@code *T} values carried as opaque Java references. */
    static boolean goPointersEqual(Object a, Object b) {
        Object an = a instanceof GoPointer ap && ap.isNil() ? null : a;
        Object bn = b instanceof GoPointer bp && bp.isNil() ? null : b;
        if (an == null || bn == null) {
            return an == null && bn == null;
        }
        if (an instanceof GoPointer ap && bn instanceof GoPointer bp) {
            return ap.sameStorageAs(bp);
        }
        return an == bn;
    }

    /**
     * Atomic {@code Value} compare-and-swap with Go interface equality
     * (P3-M10): two {@code any} values are equal per Go {@code ==} — same
     * dynamic type and equal dynamic value ({@link GoEquality#equal}) —
     * so separately boxed equal values compare equal, not just identical
     * wrapper objects.
     */
    public static boolean compareAndSwapGoValue(AtomicReference<Object> addr, Object old, Object new_) {
        for (;;) {
            Object cur = addr.get();
            if (!GoEquality.equal(cur, old)) {
                return false;
            }
            if (addr.compareAndSet(cur, new_)) {
                return true;
            }
            // Lost the race: retry against the fresh value.
        }
    }
}
