package graalgo.go.intrinsics;

import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import java.util.Random;
import java.util.zip.CRC32;
import java.util.zip.CRC32C;

/**
 * The {@code hash/crc32} assembly intrinsics ({@link CrcIntrinsics}) against the JDK's CRC-32
 * and CRC-32C: the kernels update a non-inverted CRC (the caller inverts on entry and exit), so
 * {@code ^kernel(^0, p)} is the standard checksum. The m1r gates cannot reach them while
 * {@code internal/cpu} reports no SSE 4.2 / PCLMULQDQ (docs/arch/limitations.md), hence this
 * test: random buffers at random offsets inside larger backing arrays, every length class.
 */
public final class CrcKernelTest {
    private static Slice slice(byte[] backing, int off, int len) {
        return new Slice(new Flat(null, Flat.NO_PRIMS, Flat.NO_REFS, backing), 0, 0, off, len, backing.length - off);
    }

    private static void eq(long got, long want, String what) {
        if (got != want) {
            throw new AssertionError(what + ": got " + Long.toHexString(got) + ", want " + Long.toHexString(want));
        }
    }

    public void testKernelsMatchJdk() {
        Random r = new Random(42);
        for (int iter = 0; iter < 300; iter++) {
            int len = iter < 70 ? iter : r.nextInt(4096);
            int off = r.nextInt(17);
            byte[] backing = new byte[off + len + r.nextInt(9)];
            r.nextBytes(backing);
            CRC32C c = new CRC32C();
            c.update(backing, off, len);
            eq(~CrcIntrinsics.hash_crc32_castagnoliSSE42(null, 0xFFFFFFFFL, slice(backing, off, len)) & 0xFFFFFFFFL, c.getValue(),
                            "castagnoli len " + len);
            CRC32 ieee = new CRC32();
            ieee.update(backing, off, len);
            eq(~CrcIntrinsics.hash_crc32_ieeeCLMUL(null, 0xFFFFFFFFL, slice(backing, off, len)) & 0xFFFFFFFFL, ieee.getValue(),
                            "ieee len " + len);
        }
        // Triple: three independent updates of 24*rounds bytes each, from distinct initial CRCs.
        byte[] data = new byte[3 * 24 * 5 + 3];
        r.nextBytes(data);
        long[] init = {0xFFFFFFFFL, 0x12345678L, 0};
        Object[] got = (Object[]) CrcIntrinsics.hash_crc32_castagnoliSSE42Triple(null, init[0], init[1], init[2],
                        slice(data, 1, 120), slice(data, 121, 120), slice(data, 241, 120), 5);
        for (int k = 0; k < 3; k++) {
            eq((Long) got[k], CrcIntrinsics.hash_crc32_castagnoliSSE42(null, init[k], slice(data, 1 + 120 * k, 120)), "triple " + k);
        }
    }
}
