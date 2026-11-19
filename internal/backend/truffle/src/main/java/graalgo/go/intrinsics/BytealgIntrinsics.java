package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
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

    @TruffleBoundary
    static long internal_bytealg_IndexByte(Node node, Object b, long c) {
        Slice s = (Slice) b;
        return indexByte(arr(s), s.byt, s.len, (byte) c);
    }

    @TruffleBoundary
    static long internal_bytealg_IndexByteString(Node node, Str s, long c) {
        return indexByte(s.b, s.off, s.len, (byte) c);
    }

    private static int indexByte(byte[] a, int off, int n, byte c) {
        for (int i = 0; i < n; i++) {
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
        int k = 0;
        for (int i = 0; i < n; i++) {
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
        for (int i = 0, last = n - m; i <= last; i++) {
            if (a[aoff + i] == first
                    && Arrays.equals(a, aoff + i + 1, aoff + i + m, b, boff + 1, boff + m)) {
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
