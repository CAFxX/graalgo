package graalgo.runtime;
// verifies: GG-0020 (WS G intrinsic surface)

import java.util.function.BiFunction;

import com.oracle.truffle.api.source.SourceSection;

import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoTypeDescriptor;

/**
 * Frozen runtime intrinsic surface, Phase-1 slice (WS G, GG-0020).
 *
 * <p>Every method here is part of the versioned freeze (docs/freeze-stub.md,
 * freeze v2 — v2 delta: ADR-0008 added the GoStructDescriptor elemDesc
 * parameter to makeslice/growslice): the signature is frozen, and each method is marked REAL (fully
 * implemented against the Phase-1 value model) or STUB (throws
 * {@link UnsupportedOperationException} with a Phase-2 message; listed as
 * such in the freeze doc — never a silent wrong value).
 *
 * <p>REAL in Phase 1: {@link #memmove}, {@link #memequal},
 * {@link #makeslice}, {@link #growslice}, {@link #mapAssign},
 * {@link #mapAccess}, {@link #mapDelete}, {@link #mapLen},
 * {@link #mapIterInit}, {@link #mapIterNext}, {@link #fastrand},
 * {@link #fastrandn}.
 *
 * <p>REAL since P3-M4 (WS C, ADR-0004): {@link #itabFor} (delegates to the
 * P3-M3 itab machinery via an installed provider), {@link #traceback}
 * (renders {@link GoroutineState} stacks).
 *
 * <p>Related frozen surfaces live in sibling classes:
 * {@link BuiltinPrint} (print/println), {@link Fmt} (fmt stub),
 * {@link Atomics}, {@link Timers}, {@link Scheduler}, {@link Netpoll},
 * {@link ChanOps}; panic/recover in {@link Panic} (worker-F, WS F).
 */
public final class Intrinsics {
    private Intrinsics() {}

    // ------------------------------------------------------------------
    // Memory moves (REAL).
    // ------------------------------------------------------------------

    /**
     * Copies {@code n} elements from {@code src[srcOff:]} to
     * {@code dst[dstOff:]}. The arrays must have the same component type;
     * overlapping ranges copy correctly (memmove, not memcpy). Bounds are
     * checked; violations throw {@link IndexOutOfBoundsException} (callers
     * translate to Go panics).
     */
    public static void memmove(Object dst, int dstOff, Object src, int srcOff, int n) {
        if (n < 0) {
            throw new IndexOutOfBoundsException("memmove: negative count " + n);
        }
        if (n == 0) {
            return;
        }
        int dstLen = java.lang.reflect.Array.getLength(dst);
        int srcLen = java.lang.reflect.Array.getLength(src);
        if (dstOff < 0 || srcOff < 0 || dstOff + n > dstLen || srcOff + n > srcLen) {
            throw new IndexOutOfBoundsException(
                    "memmove: range [" + srcOff + "," + (srcOff + n) + ") -> ["
                    + dstOff + "," + (dstOff + n) + ") on arrays of length "
                    + srcLen + "/" + dstLen);
        }
        // System.arraycopy has memmove (not memcpy) overlap semantics.
        System.arraycopy(src, srcOff, dst, dstOff, n);
    }

    /**
     * Reports whether {@code n} elements at the given offsets are equal.
     * Element equality is Java {@link Object#equals} on the canonical boxed
     * form (records and {@link graalgo.values.GoString} implement value
     * equality); primitive-typed arrays are compared elementwise with the
     * matching primitive equality.
     */
    public static boolean memequal(Object a, int aOff, Object b, int bOff, int n) {
        if (n < 0) {
            throw new IndexOutOfBoundsException("memequal: negative count " + n);
        }
        Class<?> ca = a.getClass().getComponentType();
        Class<?> cb = b.getClass().getComponentType();
        if (!ca.equals(cb)) {
            throw new IllegalArgumentException(
                    "memequal: component type mismatch " + ca + " vs " + cb);
        }
        if (ca == byte.class) {
            byte[] x = (byte[]) a, y = (byte[]) b;
            for (int i = 0; i < n; i++) {
                if (x[aOff + i] != y[bOff + i]) {
                    return false;
                }
            }
            return true;
        }
        if (ca == int.class) {
            int[] x = (int[]) a, y = (int[]) b;
            for (int i = 0; i < n; i++) {
                if (x[aOff + i] != y[bOff + i]) {
                    return false;
                }
            }
            return true;
        }
        if (ca == long.class) {
            long[] x = (long[]) a, y = (long[]) b;
            for (int i = 0; i < n; i++) {
                if (x[aOff + i] != y[bOff + i]) {
                    return false;
                }
            }
            return true;
        }
        if (ca == float.class) {
            float[] x = (float[]) a, y = (float[]) b;
            for (int i = 0; i < n; i++) {
                if (Float.floatToIntBits(x[aOff + i]) != Float.floatToIntBits(y[bOff + i])) {
                    return false;
                }
            }
            return true;
        }
        if (ca == double.class) {
            double[] x = (double[]) a, y = (double[]) b;
            for (int i = 0; i < n; i++) {
                if (Double.doubleToLongBits(x[aOff + i]) != Double.doubleToLongBits(y[bOff + i])) {
                    return false;
                }
            }
            return true;
        }
        if (ca == boolean.class) {
            boolean[] x = (boolean[]) a, y = (boolean[]) b;
            for (int i = 0; i < n; i++) {
                if (x[aOff + i] != y[bOff + i]) {
                    return false;
                }
            }
            return true;
        }
        Object[] x = (Object[]) a, y = (Object[]) b;
        for (int i = 0; i < n; i++) {
            Object xi = x[aOff + i], yi = y[bOff + i];
            if (xi == null ? yi != null : !xi.equals(yi)) {
                return false;
            }
        }
        return true;
    }

    // ------------------------------------------------------------------
    // Slice allocation and growth (REAL).
    // ------------------------------------------------------------------

    /**
     * {@code make([]T, len, cap)} (REAL): allocates a zeroed backing array
     * of exactly {@code cap} elements and returns a slice of length
     * {@code len}. gc's makeslice keeps the requested cap exactly (no
     * size-class rounding is observable through {@code cap()}), so this
     * does too. Out-of-range arguments are the caller's panic (the
     * {@code make} builtin node raises them as Go panics).
     */
    /**
     * Allocates a slice with the given length and capacity. For struct
     * elements, the backing array is zero-initialized (see
     * {@link #newTypedArray}); the element descriptor may be null for
     * non-struct kinds.
     */
    public static GoSlice makeslice(GoKind elemKind, graalgo.values.GoStructDescriptor elemDesc,
            int len, int cap) {
        Object array = newTypedArray(elemKind, elemDesc, cap);
        return new GoSlice(array, 0, len, cap);
    }

    /**
     * Slice growth for {@code append} (REAL): gc's {@code growslice} ported
     * exactly, so {@code cap(append(...))} sequences are bit-identical to
     * gc's. {@code old} may be {@code null} (append to a nil slice);
     * {@code newLen} is the required new length ({@code oldLen + appended}).
     *
     * <p>Growth rule (runtime/slice.go {@code nextslicecap}): double while
     * {@code oldCap < 256}, then {@code newcap += (newcap + 3*256) >> 2}
     * until sufficient; then {@code roundupsize} over the gc size-class
     * tables (ported below from internal/runtime/gc/sizeclasses.go,
     * linux/amd64) with the malloc-header adjustment for pointer-carrying
     * elements. Zero-size elements grow to exactly {@code newLen} cap.
     *
     * <p>One deliberate divergence, documented: for zero-size element
     * types gc allocates no backing memory; this port allocates a
     * zero-length Java array and reports {@code cap == newLen}. The
     * observable (len, cap, elements) triple is identical; only the
     * (unobservable in Phase 1) backing pointer differs.
     */
    public static GoSlice growslice(GoSlice old, GoKind elemKind,
            graalgo.values.GoStructDescriptor elemDesc, int newLen,
            long elemSizeBytes, boolean elemHasPointers) {
        int oldLen = old == null ? 0 : old.len();
        int oldCap = old == null ? 0 : old.cap();
        if (newLen < 0) {
            throw new IllegalArgumentException("growslice: negative new length " + newLen);
        }
        if (elemSizeBytes == 0) {
            // gc: slice{zerobase, newLen, newLen} — no backing allocation.
            return new GoSlice(newTypedArray(elemKind, elemDesc, 0), 0, newLen, newLen);
        }
        int newCap = nextSliceCap(newLen, oldCap, elemSizeBytes, elemHasPointers);
        Object array = newTypedArray(elemKind, elemDesc, newCap);
        if (old != null && oldLen > 0) {
            memmove(array, 0, old.array(), old.offset(), oldLen);
        }
        return new GoSlice(array, 0, newLen, newCap);
    }

    /**
     * gc's {@code nextslicecap} + {@code roundupsize} cap computation,
     * exposed for testing (the growth math is the falsifiable unit).
     */
    static int nextSliceCap(int newLen, int oldCap, long elemSizeBytes, boolean elemHasPointers) {
        int newcap = oldCap;
        int doublecap = newcap + newcap;
        if (newLen > doublecap) {
            newcap = newLen;
        } else {
            final int threshold = 256;
            if (oldCap < threshold) {
                newcap = doublecap;
            } else {
                for (;;) {
                    newcap += (newcap + 3 * threshold) >> 2;
                    // newLen > 0: overflow makes unsigned(newcap) >
                    // unsigned(newLen), catching both conditions.
                    if (Integer.compareUnsigned(newcap, newLen) >= 0) {
                        break;
                    }
                }
                if (newcap <= 0) {
                    newcap = newLen;
                }
            }
        }
        // roundupsize(newcap * elemSizeBytes), then back to element count.
        // The three gc cases (size==1, size==ptrSize, power-of-two, default)
        // all reduce to: capmem = roundupsize(newcap*elemSize); newcap =
        // capmem/elemSize, computed without overflow.
        long capmem = multiplyExactToLong(newcap, elemSizeBytes);
        boolean noscan = !elemHasPointers;
        long rounded = roundupsize(capmem, noscan);
        long result = rounded / elemSizeBytes;
        if (result > Integer.MAX_VALUE) {
            throw new OutOfMemoryError("growslice: capacity overflow");
        }
        return (int) result;
    }

    private static long multiplyExactToLong(int a, long b) {
        long r = (long) a * b;
        if (a != 0 && r / a != b) {
            throw new OutOfMemoryError("growslice: size overflow");
        }
        return r;
    }

    // gc size-class tables (internal/runtime/gc/sizeclasses.go, amd64).
    // SizeClassToSize[0] is unused; index = size class.
    private static final int[] SIZE_CLASS_TO_SIZE = {
        0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192,
        208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576,
        640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048,
        2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528,
        6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336,
        16384, 18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768,
    };
    private static final int[] SIZE_TO_SIZE_CLASS8 = {
        0, 1, 2, 3, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12,
        12, 13, 13, 14, 14, 15, 15, 16, 16, 17, 17, 18, 18, 19, 19, 19,
        19, 20, 20, 20, 20, 21, 21, 21, 21, 22, 22, 22, 22, 23, 23, 23,
        23, 24, 24, 24, 24, 25, 25, 25, 25, 26, 26, 26, 26, 27, 27, 27,
        27, 27, 27, 27, 27, 28, 28, 28, 28, 28, 28, 28, 28, 29, 29, 29,
        29, 29, 29, 29, 29, 30, 30, 30, 30, 30, 30, 30, 30, 31, 31, 31,
        31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 32, 32, 32,
        32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32,
    };
    private static final int[] SIZE_TO_SIZE_CLASS128 = {
        32, 33, 34, 35, 36, 37, 37, 38, 38, 39, 39, 40, 40, 40, 41, 41,
        41, 42, 43, 43, 44, 44, 44, 44, 44, 45, 45, 45, 45, 45, 45, 46,
        46, 46, 46, 47, 47, 47, 47, 47, 47, 48, 48, 48, 49, 49, 50, 51,
        51, 51, 51, 51, 51, 51, 51, 51, 51, 52, 52, 52, 52, 52, 52, 52,
        52, 52, 52, 53, 53, 54, 54, 54, 54, 55, 55, 55, 55, 55, 56, 56,
        56, 56, 56, 56, 56, 56, 56, 56, 56, 57, 57, 57, 57, 57, 57, 57,
        57, 57, 57, 58, 58, 58, 58, 58, 58, 59, 59, 59, 59, 59, 59, 59,
        59, 59, 59, 59, 59, 59, 59, 59, 59, 60, 60, 60, 60, 60, 60, 60,
        60, 60, 60, 60, 60, 60, 60, 60, 60, 61, 61, 61, 61, 61, 62, 62,
        62, 62, 62, 62, 62, 62, 62, 62, 62, 63, 63, 63, 63, 63, 63, 63,
        63, 63, 63, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
        64, 64, 64, 64, 64, 64, 64, 64, 64, 65, 65, 65, 65, 65, 65, 65,
        65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 66, 66,
        66, 66, 66, 66, 66, 66, 66, 66, 66, 67, 67, 67, 67, 67, 67, 67,
        67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67,
        67, 67, 67, 67, 67, 67, 67,
    };

    private static final long MAX_SMALL_SIZE = 32768;
    private static final long SMALL_SIZE_MAX = 1024;
    private static final long SMALL_SIZE_DIV = 8;
    private static final long LARGE_SIZE_DIV = 128;
    private static final long MALLOC_HEADER_SIZE = 8;
    private static final long MIN_SIZE_FOR_MALLOC_HEADER = 512; // 8*64 on amd64
    private static final long PAGE_SIZE = 8192;

    /**
     * gc's {@code roundupsize} (runtime/msize.go), unsigned 64-bit
     * arithmetic. {@code noscan} = element type carries no pointers.
     */
    static long roundupsize(long size, boolean noscan) {
        long reqSize = size;
        if (Long.compareUnsigned(reqSize, MAX_SMALL_SIZE - MALLOC_HEADER_SIZE) <= 0) {
            if (!noscan && Long.compareUnsigned(reqSize, MIN_SIZE_FOR_MALLOC_HEADER) > 0) {
                reqSize += MALLOC_HEADER_SIZE;
            }
            long header = reqSize - size; // 0 or 8; subtracted back below
            if (Long.compareUnsigned(reqSize, SMALL_SIZE_MAX - 8) <= 0) {
                int idx = (int) ((reqSize + SMALL_SIZE_DIV - 1) / SMALL_SIZE_DIV);
                return SIZE_CLASS_TO_SIZE[SIZE_TO_SIZE_CLASS8[idx]] - header;
            }
            int idx = (int) ((reqSize - SMALL_SIZE_MAX + LARGE_SIZE_DIV - 1) / LARGE_SIZE_DIV);
            return SIZE_CLASS_TO_SIZE[SIZE_TO_SIZE_CLASS128[idx]] - header;
        }
        reqSize += PAGE_SIZE - 1;
        if (Long.compareUnsigned(reqSize, size) < 0) {
            return size;
        }
        return reqSize & ~(PAGE_SIZE - 1);
    }

    /** Fresh zeroed typed array for slice backing (D5 array mapping). */
    /**
     * Allocates a typed Java array for slice elements. For STRUCT elements,
     * each slot is pre-initialized to the zero-valued GoStruct (Go's
     * {@code make([]T, n)} zero-initializes; a null slot would NPE on
     * {@code s[i].field} access). Requires the element descriptor for
     * struct kinds; may be null for non-struct kinds.
     */
    static Object newTypedArray(GoKind elemKind, graalgo.values.GoStructDescriptor elemDesc, int len) {
        return switch (elemKind) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> new int[len];
            case INT64, UINT64, UNTYPED_INT -> new long[len];
            case FLOAT32 -> new float[len];
            case FLOAT64, UNTYPED_FLOAT -> new double[len];
            case BOOL, UNTYPED_BOOL -> new boolean[len];
            case COMPLEX64 -> new graalgo.values.GoComplex32[len];
            case COMPLEX128, UNTYPED_COMPLEX -> new graalgo.values.GoComplex64[len];
            case STRUCT -> {
                Object[] arr = new Object[len];
                if (elemDesc != null) {
                    for (int i = 0; i < len; i++) {
                        arr[i] = graalgo.values.GoStruct.zero(elemDesc);
                    }
                }
                yield arr;
            }
            default -> new Object[len];
        };
    }

    // ------------------------------------------------------------------
    // Map operations (REAL — thin over the Phase-1 GoMap).
    // ------------------------------------------------------------------

    /** {@code m[k] = v}: assign (insert or overwrite). Keys are already in canonical boxed form. */
    public static void mapAssign(GoMap m, Object key, Object value) {
        m.put(key, value);
    }

    /**
     * Map lookup returning {@code null} when absent — callers apply the
     * comma-ok / zero-value rule (see IndexNode).
     */
    public static Object mapAccess(GoMap m, Object key) {
        return m.get(key);
    }

    /** {@code delete(m, k)}: no-op on absent keys; {@code null} map is a no-op (callers check). */
    public static void mapDelete(GoMap m, Object key) {
        m.delete(key);
    }

    /** {@code len(m)}. */
    public static int mapLen(GoMap m) {
        return m.size();
    }

    // ------------------------------------------------------------------
    // Slice helpers for builtins (WS G).
    // ------------------------------------------------------------------

    /** Element size in bytes for a GoKind (D5 mapping). */
    public static long elemSizeBytes(GoKind kind) {
        return switch (kind) {
            case INT8, UINT8, BOOL -> 1;
            case INT16, UINT16 -> 2;
            case INT32, UINT32, FLOAT32 -> 4;
            case INT64, UINT64, FLOAT64, COMPLEX64 -> 8;
            case COMPLEX128 -> 16;
            case STRING -> 16; // ptr + len
            case SLICE -> 24; // ptr + len + cap
            case MAP, POINTER, FUNC, CHAN, UNSAFE_POINTER, INTERFACE -> 8;
            default -> 0; // structs computed by caller; arrays by caller
        };
    }

    /** Whether values of this kind contain pointers (for GC scanning). */
    public static boolean elemHasPointers(GoKind kind) {
        return switch (kind) {
            case STRING, SLICE, MAP, POINTER, FUNC, CHAN, INTERFACE, UNSAFE_POINTER -> true;
            default -> false;
        };
    }

    /**
     * The ABI size in bytes of a struct type (P3-M4, WS C): Go field
     * layout — fields in declaration order, each aligned to its natural
     * alignment, struct size rounded up to the max field alignment.
     * Only flat structs with basic-kind fields are supported; nested
     * structs/arrays fail fast (their sizes are not on the wire).
     */
    public static long structSizeBytes(graalgo.values.GoStructDescriptor desc) {
        long offset = 0;
        long maxAlign = 1;
        for (graalgo.values.GoStructDescriptor.Field f : desc.fields()) {
            long fieldSize = elemSizeBytes(f.kind());
            if (fieldSize == 0) {
                throw new graalgo.frontend.FrontendException("structSizeBytes: field \"" + f.name()
                        + "\" has non-basic kind " + f.kind()
                        + " (nested structs/arrays not in M4 scope)");
            }
            long align = Math.min(fieldSize, 8);
            maxAlign = Math.max(maxAlign, align);
            offset = (offset + align - 1) / align * align;
            offset += fieldSize;
        }
        return (offset + maxAlign - 1) / maxAlign * maxAlign;
    }

    /**
     * Convenience overload for {@code append}: grows {@code old} by
     * {@code additional} elements. {@code old} may be null (append to nil).
     * No struct descriptor (struct slots are not zero-initialized; the
     * caller must provide elements for the grown region).
     */
    public static GoSlice growslice(GoSlice old, GoKind elemKind, int additional) {
        int oldLen = old == null ? 0 : old.len();
        return growslice(old, elemKind, null, oldLen + additional,
                elemSizeBytes(elemKind), elemHasPointers(elemKind));
    }

    /**
     * The {@code copy} builtin (REAL): copies {@code min(len(dst), len(src))}
     * elements from {@code src} to {@code dst}, returns the count. Overlap
     * is safe (memmove semantics). Neither slice header is modified.
     *
     * <p>P3-M7: a nil dst or src is legal Go (copies 0 elements) — the
     * GoSlice reference is null for nil slices, so null-check both.
     */
    public static int slicecopy(GoSlice dst, GoSlice src, GoKind elemKind) {
        int n = Math.min(dst == null ? 0 : dst.len(), src == null ? 0 : src.len());
        if (n > 0) {
            memmove(dst.array(), dst.offset(), src.array(), src.offset(), n, elemKind);
        }
        return n;
    }

    /** Typed memmove of {@code n} elements of {@code elemKind}. */
    private static void memmove(Object dstArr, int dstOff, Object srcArr, int srcOff,
            int n, GoKind elemKind) {
        switch (elemKind) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> {
                int[] d = (int[]) dstArr;
                int[] s = (int[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
            case INT64, UINT64 -> {
                long[] d = (long[]) dstArr;
                long[] s = (long[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
            case FLOAT32 -> {
                float[] d = (float[]) dstArr;
                float[] s = (float[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
            case FLOAT64 -> {
                double[] d = (double[]) dstArr;
                double[] s = (double[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
            case BOOL -> {
                boolean[] d = (boolean[]) dstArr;
                boolean[] s = (boolean[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
            case COMPLEX64 -> {
                graalgo.values.GoComplex32[] d = (graalgo.values.GoComplex32[]) dstArr;
                graalgo.values.GoComplex32[] s = (graalgo.values.GoComplex32[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
            case COMPLEX128 -> {
                graalgo.values.GoComplex64[] d = (graalgo.values.GoComplex64[]) dstArr;
                graalgo.values.GoComplex64[] s = (graalgo.values.GoComplex64[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
            default -> {
                Object[] d = (Object[]) dstArr;
                Object[] s = (Object[]) srcArr;
                System.arraycopy(s, srcOff, d, dstOff, n);
            }
        }
    }

    /** Iterator state for {@code range} over maps: a keys snapshot (insertion-order-independent). */
    public static Object[] mapIterInit(GoMap m) {
        return m.keysSnapshot();
    }

    /** Next key from an iterator snapshot, or {@code null} when exhausted. */
    public static Object mapIterNext(Object[] snapshot, int index) {
        return index < snapshot.length ? snapshot[index] : null;
    }

    // ------------------------------------------------------------------
    // Fast random (REAL).
    // ------------------------------------------------------------------

    // wyrand (wangyi-fudan/wyhash), as in runtime.cheaprandu64; the seed is
    // per-process (gc seeds per-M from system entropy), so only the
    // algorithm — not the sequence — is pinned.
    private static long wyrandState = System.nanoTime() ^ 0x9E3779B97F4A7C15L;

    /** 64-bit wyrand step (REAL). */
    public static synchronized long fastrand64() {
        wyrandState += 0xA0761D6478BD642FL;
        long s = wyrandState;
        long x = s ^ 0xE7037ED1A0B428DBL;
        // Unsigned 128-bit multiply (Go's bits.Mul64), keeping the hi and
        // lo 64-bit halves. Math.multiplyHigh is *signed*-high and would
        // be wrong here; the schoolbook form below is unsigned.
        long s0 = s & 0xFFFFFFFFL, s1 = s >>> 32;
        long x0 = x & 0xFFFFFFFFL, x1 = x >>> 32;
        long p1 = s0 * x1, p2 = s1 * x0, p3 = s1 * x1;
        long mid = (s0 * x0 >>> 32) + (p1 & 0xFFFFFFFFL) + (p2 & 0xFFFFFFFFL);
        long hi = p3 + (p1 >>> 32) + (p2 >>> 32) + (mid >>> 32);
        long lo = s * x; // low 64 bits: same for signed and unsigned
        return hi ^ lo;
    }

    /** 32-bit fast random (REAL): low 32 bits of a wyrand step. */
    public static int fastrand() {
        return (int) fastrand64();
    }

    /**
     * {@code fastrandn(n)}: unbiased-below-n via Lemire's multiply-shift
     * (REAL). P3-M4 remediation (P1-1): no positivity guard — gc 1.27's
     * {@code randn} has no check, and the multiply-shift with n=0 yields
     * 0, so {@code fastrandn(0) == 0} (a guard would turn a defined gc
     * result into a fatal exception).
     */
    public static int fastrandn(int n) {
        return (int) ((Integer.toUnsignedLong(fastrand()) * Integer.toUnsignedLong(n)) >>> 32);
    }

    // ------------------------------------------------------------------
    // Itab + traceback (P3-M4, WS C, ADR-0004; GG-0020 completion).
    // ------------------------------------------------------------------

    /**
     * Provider hook for {@link #itabFor} (P3-M4, ADR-0004): phase-1 has no
     * method tables, so the real lookup lives in phase-3
     * ({@code graalgo.iface.IfaceMethodTable}, P3-M3), which installs its
     * provider at launcher startup. The frozen signature is unchanged;
     * STUB bodies are INTERNAL per freeze §8.
     */
    private static volatile BiFunction<GoTypeDescriptor, GoTypeDescriptor, Object> itabProvider;

    /**
     * Installs the itab provider (phase-3 launcher). A null provider
     * restores the unimplemented state.
     */
    public static void setItabProvider(
            BiFunction<GoTypeDescriptor, GoTypeDescriptor, Object> provider) {
        itabProvider = provider;
    }

    /**
     * Itab lookup (interface method table for a concrete type) — REAL via
     * the installed provider (P3-M3 itab machinery). Without a provider
     * (phase-1-only mode, which has no method tables) throws, as before.
     */
    public static Object itabFor(GoTypeDescriptor descriptor, GoTypeDescriptor iface) {
        BiFunction<GoTypeDescriptor, GoTypeDescriptor, Object> p = itabProvider;
        if (p == null) {
            throw new UnsupportedOperationException(
                    "itab lookup needs the P3-M3 itab machinery (GG-0020): no provider installed");
        }
        return p.apply(descriptor, iface);
    }

    /**
     * Goroutine traceback (REAL): renders the current goroutine's Go call
     * stack in gc's single-goroutine shape. Works through
     * {@link GoroutineState#current()}, so under phase-2/3 classpaths it
     * sees the virtual-thread-scoped holder.
     */
    public static String traceback() {
        GoroutineState state = GoroutineState.current();
        if (state == null) {
            throw new IllegalStateException("traceback: no goroutine is running on this thread");
        }
        return renderGoStack(1, state.goStack(), null);
    }

    /**
     * Renders a Go stack in gc's single-goroutine shape:
     * {@code goroutine <id> [running]:} followed by one {@code name()} line
     * and one {@code \tfile:line} line per frame, innermost first
     * (shared with the panic path). Known divergences from gc, documented:
     * no {@code +0x} pc offsets (no machine code), no argument lists
     * (graalgo frames carry no arg words), unqualified frame names (the
     * wire carries no package path), no {@code created by} line (the
     * scheduler does not record creators). Each divergence has an
     * explicit re-test trigger in
     * {@code test/corpus/traceback/README.md} ("Re-test
     * triggers").
     *
     * @param goroutineId the id to print (gc: the main goroutine is 1)
     * @param top the innermost frame (null renders an empty stack)
     * @param throwSiteOverride source section for the innermost frame
     *        (the panic throw site), or null to use the frame's own
     */
    public static String renderGoStack(long goroutineId, GoStackFrame top,
            SourceSection throwSiteOverride) {
        StringBuilder sb = new StringBuilder();
        sb.append("goroutine ").append(goroutineId).append(" [running]:\n");
        GoStackFrame f = top;
        boolean first = true;
        while (f != null && !f.isEmpty()) {
            String name = f.funcName();
            // gc qualifies main as main.main; funclits as main.main.funcN.
            if ("main".equals(name)) {
                name = "main.main";
            }
            sb.append(name).append("()\n");
            SourceSection sec = f.section();
            if (first && throwSiteOverride != null) {
                sec = throwSiteOverride;
            }
            first = false;
            if (sec != null) {
                sb.append('\t').append(sec.getSource().getName()).append(':')
                        .append(sec.getStartLine()).append('\n');
            }
            f = f.parent();
        }
        return sb.toString();
    }
}
