package graalgo.go.intrinsics;

import graalgo.go.values.Str;
import java.util.Random;

/**
 * The word-at-a-time {@code internal/bytealg} kernels ({@code IndexByte}, {@code Count},
 * {@code Index}) against a byte-by-byte reference, over random windows of random arrays on a
 * small alphabet (so matches, near misses and bytes with the high bit set are all frequent):
 * every window offset and length class, including lengths below one word and tails.
 */
public final class BytealgKernelTest {
    private static final byte[] ALPHABET = {0, 1, 'a', 'b', (byte) 0x80, (byte) 0xFF, (byte) 0x7F};

    private static int refIndexByte(byte[] a, int off, int n, byte c) {
        for (int i = 0; i < n; i++) {
            if (a[off + i] == c) {
                return i;
            }
        }
        return -1;
    }

    private static int refCount(byte[] a, int off, int n, byte c) {
        int k = 0;
        for (int i = 0; i < n; i++) {
            if (a[off + i] == c) {
                k++;
            }
        }
        return k;
    }

    private static int refIndex(byte[] a, int aoff, int n, byte[] b, int boff, int m) {
        outer:
        for (int i = 0; i + m <= n; i++) {
            for (int j = 0; j < m; j++) {
                if (a[aoff + i + j] != b[boff + j]) {
                    continue outer;
                }
            }
            return i;
        }
        return -1;
    }

    private static byte[] random(Random r, int n, int alphabet) {
        byte[] a = new byte[n];
        for (int i = 0; i < n; i++) {
            a[i] = ALPHABET[r.nextInt(alphabet)];
        }
        return a;
    }

    private static void eq(long got, long want, String what) {
        if (got != want) {
            throw new AssertionError(what + ": got " + got + ", want " + want);
        }
    }

    public void testKernelsMatchReference() {
        Random r = new Random(42);
        for (int iter = 0; iter < 20000; iter++) {
            int alphabet = 1 + r.nextInt(ALPHABET.length);
            byte[] a = random(r, 1 + r.nextInt(80), alphabet);
            int off = r.nextInt(a.length);
            int n = r.nextInt(a.length - off + 1);
            Str s = new Str(a, off, n);
            byte c = ALPHABET[r.nextInt(ALPHABET.length)];
            String at = " (iter " + iter + ", off " + off + ", n " + n + ", c " + c + ")";
            eq(BytealgIntrinsics.internal_bytealg_IndexByteString(null, s, c), refIndexByte(a, off, n, c),
                    "IndexByteString" + at);
            eq(BytealgIntrinsics.internal_bytealg_CountString(null, s, c), refCount(a, off, n, c), "CountString" + at);
            byte[] p = random(r, 1 + r.nextInt(4), alphabet);
            int poff = r.nextInt(p.length);
            int m = r.nextInt(p.length - poff + 1);
            eq(BytealgIntrinsics.internal_bytealg_IndexString(null, s, new Str(p, poff, m)),
                    m == 0 ? 0 : refIndex(a, off, n, p, poff, m), "IndexString" + at);
        }
    }
}
