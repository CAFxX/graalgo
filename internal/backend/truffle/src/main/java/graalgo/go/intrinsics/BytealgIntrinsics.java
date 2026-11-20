package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import java.lang.invoke.MethodHandles;
import java.lang.invoke.VarHandle;
import java.nio.ByteOrder;
import java.util.Arrays;

/**
 * Package {@code internal/bytealg}: the functions upstream implements in amd64 assembly
 * ({@code compare_native.go}, {@code count_native.go}, {@code index_native.go},
 * {@code indexbyte_native.go}) and {@code MakeNoZero}, which the runtime provides by
 * {@code //go:linkname}; registered in {@code internal/toolexec/intrinsics/registry.txt}.
 * Everything else in the package is upstream's Go.
 *
 * <p>Byte data lives in the byte region of the flat value model (GG-0072, ADR-0015 decision 3):
 * a {@code []byte} is {@code base.bytes[byt .. byt+len)}, a {@code string} is
 * {@code b[off .. off+len)}. The comparisons use the JDK's vectorized range operations
 * ({@link Arrays#mismatch}, {@link Arrays#compareUnsigned}, {@link Arrays#equals}).
 *
 * <p>The assembly's preconditions are wider here: {@code Index}/{@code IndexString} accept any
 * pattern length (upstream calls them only for {@code 2 <= len(b) <= MaxLen}); the result is the
 * first index either way.
 */
final class BytealgIntrinsics {
    private BytealgIntrinsics() {}

    private static final byte[] NONE = new byte[0];

    private static byte[] arr(Slice s) {
        return s.base == null ? NONE : s.base.bytes;
    }

    /** {@code bytes.Compare}'s kernel: -1, 0, +1 by unsigned lexicographic order. */
    @TruffleBoundary
    static long internal_bytealg_Compare(Node node, Object a, Object b) {
        Slice x = (Slice) a, y = (Slice) b;
        return Integer.signum(Arrays.compareUnsigned(arr(x), x.byt, x.byt + x.len, arr(y), y.byt, y.byt + y.len));
    }

    /** {@code runtime.cmpstring} (assembly in {@code internal/bytealg}): as {@code Compare}. */
    @TruffleBoundary
    static long runtime_cmpstring(Node node, Str a, Str b) {
        return Integer.signum(Arrays.compareUnsigned(a.b, a.off, a.off + a.len, b.b, b.off, b.off + b.len));
    }

    @TruffleBoundary
    static long internal_bytealg_IndexByte(Node node, Object b, long c) {
        Slice s = (Slice) b;
        return indexByte(arr(s), s.byt, s.len, (byte) c);
    }

    @TruffleBoundary
    static long internal_bytealg_IndexByteString(Node node, Str s, long c) {
        return indexByte(s.b, s.off, s.len, (byte) c);
    }

    /** Little-endian 8-byte loads from a {@code byte[]} (one unaligned load when compiled). */
    private static final VarHandle LONGS = MethodHandles.byteArrayViewVarHandle(long[].class, ByteOrder.LITTLE_ENDIAN);
    private static final long LOW7 = 0x7F7F7F7F7F7F7F7FL;
    private static final long HIGH = 0x8080808080808080L;
    private static final long ONES = 0x0101010101010101L;

    /**
     * The high bit of every byte of {@code v} that is zero, and only of those: the 7 low bits
     * plus 0x7F carry into the high bit exactly when they are not all zero, so no borrow or
     * carry crosses a byte (word-at-a-time search; gc's kernels are SIMD assembly).
     */
    private static long zeros(long v) {
        return ~(((v & LOW7) + LOW7) | v | LOW7);
    }

    private static int indexByte(byte[] a, int off, int n, byte c) {
        long pat = (c & 0xFFL) * ONES;
        int i = 0;
        for (; i + 8 <= n; i += 8) {
            long z = zeros((long) LONGS.get(a, off + i) ^ pat);
            if (z != 0) {
                return i + (Long.numberOfTrailingZeros(z) >>> 3);
            }
        }
        for (; i < n; i++) {
            if (a[off + i] == c) {
                return i;
            }
        }
        return -1;
    }

    @TruffleBoundary
    static long internal_bytealg_Count(Node node, Object b, long c) {
        Slice s = (Slice) b;
        return count(arr(s), s.byt, s.len, (byte) c);
    }

    @TruffleBoundary
    static long internal_bytealg_CountString(Node node, Str s, long c) {
        return count(s.b, s.off, s.len, (byte) c);
    }

    private static int count(byte[] a, int off, int n, byte c) {
        long pat = (c & 0xFFL) * ONES;
        int k = 0;
        int i = 0;
        for (; i + 8 <= n; i += 8) {
            k += Long.bitCount(zeros((long) LONGS.get(a, off + i) ^ pat));
        }
        for (; i < n; i++) {
            if (a[off + i] == c) {
                k++;
            }
        }
        return k;
    }

    @TruffleBoundary
    static long internal_bytealg_Index(Node node, Object a, Object b) {
        Slice x = (Slice) a, y = (Slice) b;
        return index(arr(x), x.byt, x.len, arr(y), y.byt, y.len);
    }

    @TruffleBoundary
    static long internal_bytealg_IndexString(Node node, Str a, Str b) {
        return index(a.b, a.off, a.len, b.b, b.off, b.len);
    }

    /** The first i with {@code a[i:i+m] == b}, or -1. */
    private static int index(byte[] a, int aoff, int n, byte[] b, int boff, int m) {
        if (m == 0) {
            return 0;
        }
        byte first = b[boff];
        int last = n - m;
        for (int i = 0; i <= last; i++) {
            // The next candidate: a word-at-a-time scan for the first byte.
            int j = indexByte(a, aoff + i, last - i + 1, first);
            if (j < 0) {
                return -1;
            }
            i += j;
            if (Arrays.equals(a, aoff + i + 1, aoff + i + m, b, boff + 1, boff + m)) {
                return i;
            }
        }
        return -1;
    }

    /**
     * gc's {@code bytealg_MakeNoZero}: a {@code []byte} of length n whose capacity is gc's
     * {@code roundupsize(n)} (observable through {@code strings.Builder.Cap}); the bytes are
     * zero, which the contract allows.
     */
    @TruffleBoundary
    static Object internal_bytealg_MakeNoZero(Node node, long n) {
        if (n < 0 || n > Integer.MAX_VALUE - 8) {
            throw Panics.error(node, "makeslice: len out of range");
        }
        // nextSliceCap from an empty slice is exactly roundupsize(n) for one-byte elements.
        int cap = n == 0 ? 0 : graalgo.runtime.Intrinsics.nextSliceCap((int) n, 0, 1, false);
        GoType backing = GoProcess.get(node).types.basic(GoType.UINT8).backing();
        return new Slice(new Flat(backing, Flat.NO_PRIMS, Flat.NO_REFS, new byte[cap]), 0, 0, 0, (int) n, cap);
    }
}
