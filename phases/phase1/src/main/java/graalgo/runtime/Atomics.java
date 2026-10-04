package graalgo.runtime;

import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Frozen atomic intrinsic surface (WS G, GG-0020, REAL).
 *
 * <p>Phase-1 implementations over {@link AtomicInteger}/{@link AtomicLong};
 * the Phase-1 memory model is single-threaded, so these are sequentially
 * consistent by construction. Backed by {@code java.util.concurrent.atomic}
 * (no deprecated APIs).
 */
public final class Atomics {
    private Atomics() {}

    // -- int32 ----------------------------------------------------------

    /** Atomic int32 load. */
    public static int loadInt32(AtomicInteger addr) {
        return addr.get();
    }

    /** Atomic int32 store. */
    public static void storeInt32(AtomicInteger addr, int v) {
        addr.set(v);
    }

    /** Atomic int32 add; returns the new value. */
    public static int addInt32(AtomicInteger addr, int delta) {
        return addr.addAndGet(delta);
    }

    /** Atomic int32 compare-and-swap; returns whether the swap happened. */
    public static boolean compareAndSwapInt32(AtomicInteger addr, int old, int new_) {
        return addr.compareAndSet(old, new_);
    }

    /** Atomic int32 swap; returns the previous value. */
    public static int swapInt32(AtomicInteger addr, int new_) {
        return addr.getAndSet(new_);
    }

    // -- int64 ----------------------------------------------------------

    /** Atomic int64 load. */
    public static long loadInt64(AtomicLong addr) {
        return addr.get();
    }

    /** Atomic int64 store. */
    public static void storeInt64(AtomicLong addr, long v) {
        addr.set(v);
    }

    /** Atomic int64 add; returns the new value. */
    public static long addInt64(AtomicLong addr, long delta) {
        return addr.addAndGet(delta);
    }

    /** Atomic int64 compare-and-swap; returns whether the swap happened. */
    public static boolean compareAndSwapInt64(AtomicLong addr, long old, long new_) {
        return addr.compareAndSet(old, new_);
    }

    /** Atomic int64 swap; returns the previous value. */
    public static long swapInt64(AtomicLong addr, long new_) {
        return addr.getAndSet(new_);
    }

    // -- pointer-sized (uintptr) -----------------------------------------

    /** Atomic uintptr load. */
    public static long loadUintptr(AtomicLong addr) {
        return addr.get();
    }

    /** Atomic uintptr store. */
    public static void storeUintptr(AtomicLong addr, long v) {
        addr.set(v);
    }

    /** Atomic uintptr compare-and-swap. */
    public static boolean compareAndSwapUintptr(AtomicLong addr, long old, long new_) {
        return addr.compareAndSet(old, new_);
    }

    // -- bool -------------------------------------------------------------

    /**
     * Atomic bool load. Booleans ride on an {@link AtomicInteger}
     * (0/1); Phase 2 may repack this.
     */
    public static boolean loadBool(AtomicInteger addr) {
        return addr.get() != 0;
    }

    /** Atomic bool store. */
    public static void storeBool(AtomicInteger addr, boolean v) {
        addr.set(v ? 1 : 0);
    }

    /** Atomic bool compare-and-swap. */
    public static boolean compareAndSwapBool(AtomicInteger addr, boolean old, boolean new_) {
        return addr.compareAndSet(old ? 1 : 0, new_ ? 1 : 0);
    }
}
