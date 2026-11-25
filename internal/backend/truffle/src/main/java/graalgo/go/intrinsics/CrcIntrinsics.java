package graalgo.go.intrinsics;

import com.oracle.truffle.api.nodes.Node;
import graalgo.go.values.Slice;

/**
 * Package {@code hash/crc32}: the functions upstream implements in amd64 assembly
 * ({@code crc32_amd64.s}: the SSE 4.2 {@code CRC32} and {@code PCLMULQDQ} kernels); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. They update a <em>non-inverted</em> CRC
 * (the instruction's contract: the caller does the {@code ^crc} on entry and exit), here with
 * the reflected table of the polynomial, byte at a time.
 *
 * <p>Upstream calls them only when {@code internal/cpu} reports SSE 4.2 (Castagnoli) or
 * PCLMULQDQ + SSE 4.1 (IEEE); graalgo's {@code internal/cpu} reports no optional feature
 * (docs/arch/limitations.md), so today {@code crc32} takes its portable slicing-by-8 path and
 * these run only if that changes. They are exact either way: the link needs them because the
 * feature-guarded calls are statically reachable.
 */
final class CrcIntrinsics {
    private CrcIntrinsics() {}

    /** Castagnoli, reflected (upstream {@code crc32.Castagnoli} reversed). */
    private static final int[] CASTAGNOLI = table(0x82F63B78);
    /** IEEE, reflected (upstream {@code crc32.IEEE}). */
    private static final int[] IEEE = table(0xEDB88320);

    private static int[] table(int poly) {
        int[] t = new int[256];
        for (int i = 0; i < 256; i++) {
            int c = i;
            for (int j = 0; j < 8; j++) {
                c = (c & 1) != 0 ? (c >>> 1) ^ poly : c >>> 1;
            }
            t[i] = c;
        }
        return t;
    }

    private static int update(int[] t, int crc, Slice p, int off, int n) {
        byte[] b = p.base() == null ? null : p.base().bytes;
        int base = p.byt() + off;
        for (int i = 0; i < n; i++) {
            crc = t[(crc ^ b[base + i]) & 0xFF] ^ (crc >>> 8);
        }
        return crc;
    }

    /** {@code castagnoliSSE42(crc uint32, p []byte) uint32}. */
    static long hash_crc32_castagnoliSSE42(Node node, long crc, Object p) {
        Slice s = (Slice) p;
        return update(CASTAGNOLI, (int) crc, s, 0, s.len()) & 0xFFFFFFFFL;
    }

    /**
     * {@code castagnoliSSE42Triple(crcA, crcB, crcC uint32, a, b, c []byte, rounds uint32) (retA,
     * retB, retC uint32)}: three independent updates of {@code 24*rounds} bytes each.
     */
    static Object hash_crc32_castagnoliSSE42Triple(Node node, long crcA, long crcB, long crcC, Object a, Object b, Object c,
                    long rounds) {
        int n = (int) (24 * rounds);
        return new Object[] {
                        update(CASTAGNOLI, (int) crcA, (Slice) a, 0, n) & 0xFFFFFFFFL,
                        update(CASTAGNOLI, (int) crcB, (Slice) b, 0, n) & 0xFFFFFFFFL,
                        update(CASTAGNOLI, (int) crcC, (Slice) c, 0, n) & 0xFFFFFFFFL};
    }

    /** {@code ieeeCLMUL(crc uint32, p []byte) uint32}: the IEEE update of all of p (len a multiple of 16). */
    static long hash_crc32_ieeeCLMUL(Node node, long crc, Object p) {
        Slice s = (Slice) p;
        return update(IEEE, (int) crc, s, 0, s.len()) & 0xFFFFFFFFL;
    }
}
