package graalgo.go.intrinsics;

import com.oracle.truffle.api.nodes.Node;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;

/**
 * Package {@code internal/chacha8rand}: {@code block}, which upstream implements in amd64
 * assembly ({@code chacha8_amd64.s}, no {@code purego} variant); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. It is upstream's {@code block_generic}
 * (the assembly's specification, {@code chacha8_generic.go}) over the scalar region: the
 * {@code [32]uint64} buffer is read as gc lays it out, {@code [16][4]uint32} little-endian —
 * uint32 {@code k} is the low ({@code k} even) or high half of word {@code k/2}.
 */
final class ChachaIntrinsics {
    private ChachaIntrinsics() {}

    private static long[] words(Object p) {
        return p instanceof Ptr q ? q.obj().prims : ((Flat) p).prims;
    }

    private static int base(Object p) {
        return p instanceof Ptr q ? q.prim() : 0;
    }

    /** {@code block(seed *[4]uint64, blocks *[32]uint64, counter uint32)}. */
    static void internal_chacha8rand_block(Node node, Object seedPtr, Object bufPtr, long counter) {
        long[] sw = words(seedPtr);
        int so = base(seedPtr);
        long[] buf = words(bufPtr);
        int bo = base(bufPtr);
        int[] b = new int[64];
        // setup: rows 0-3 constants, 4-11 the seed words, 12 the four counters, 13-15 zero.
        int[] consts = {0x61707865, 0x3320646e, 0x79622d32, 0x6b206574};
        for (int i = 0; i < 4; i++) {
            for (int r = 0; r < 4; r++) {
                b[r * 4 + i] = consts[r];
            }
            for (int k = 0; k < 8; k++) {
                long s = sw[so + k / 2];
                b[(4 + k) * 4 + i] = (int) ((k & 1) == 0 ? s : s >>> 32);
            }
            b[12 * 4 + i] = (int) counter + i;
        }
        int[] out = new int[64];
        for (int i = 0; i < 4; i++) {
            int[] x = new int[16];
            for (int r = 0; r < 16; r++) {
                x[r] = b[r * 4 + i];
            }
            for (int round = 0; round < 4; round++) {
                qr(x, 0, 4, 8, 12);
                qr(x, 1, 5, 9, 13);
                qr(x, 2, 6, 10, 14);
                qr(x, 3, 7, 11, 15);
                qr(x, 0, 5, 10, 15);
                qr(x, 1, 6, 11, 12);
                qr(x, 2, 7, 8, 13);
                qr(x, 3, 4, 9, 14);
            }
            for (int r = 0; r < 16; r++) {
                // rows 4-11 (the key) are added back; the others are stored as computed
                out[r * 4 + i] = r >= 4 && r <= 11 ? b[r * 4 + i] + x[r] : x[r];
            }
        }
        for (int w = 0; w < 32; w++) {
            buf[bo + w] = (out[2 * w] & 0xFFFFFFFFL) | ((long) out[2 * w + 1] << 32);
        }
    }

    private static void qr(int[] x, int ia, int ib, int ic, int id) {
        int a = x[ia], b = x[ib], c = x[ic], d = x[id];
        a += b;
        d ^= a;
        d = Integer.rotateLeft(d, 16);
        c += d;
        b ^= c;
        b = Integer.rotateLeft(b, 12);
        a += b;
        d ^= a;
        d = Integer.rotateLeft(d, 8);
        c += d;
        b ^= c;
        b = Integer.rotateLeft(b, 7);
        x[ia] = a;
        x[ib] = b;
        x[ic] = c;
        x[id] = d;
    }
}
