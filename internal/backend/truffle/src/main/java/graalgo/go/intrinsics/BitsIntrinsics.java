package graalgo.go.intrinsics;

import com.oracle.truffle.api.nodes.Node;

/**
 * The functions of {@code math/bits} that gc's compiler replaces by an instruction on amd64
 * (LZCNT/BSR, TZCNT/BSF, POPCNT, ROL, BSWAP): the JDK methods below are intrinsified to the
 * same instructions. Registered in {@code internal/toolexec/intrinsics/registry.txt}.
 *
 * <p>Carriers: a {@code uint64} travels as its bits in a {@code long}, a {@code uint32} as a
 * zero-extended {@code long}, an {@code int} as a {@code long}.
 */
final class BitsIntrinsics {
    private BitsIntrinsics() {}

    static long math_bits_Len64(Node node, long x) {
        return 64 - Long.numberOfLeadingZeros(x);
    }

    static long math_bits_Len32(Node node, long x) {
        return 32 - Integer.numberOfLeadingZeros((int) x);
    }

    static long math_bits_TrailingZeros64(Node node, long x) {
        return Long.numberOfTrailingZeros(x);
    }

    static long math_bits_TrailingZeros32(Node node, long x) {
        return Integer.numberOfTrailingZeros((int) x);
    }

    static long math_bits_OnesCount64(Node node, long x) {
        return Long.bitCount(x);
    }

    static long math_bits_OnesCount32(Node node, long x) {
        return Integer.bitCount((int) x);
    }

    // Go rotates by k mod the width, negative k rotating right — as the JDK's rotations do.
    static long math_bits_RotateLeft64(Node node, long x, long k) {
        return Long.rotateLeft(x, (int) k);
    }

    static long math_bits_RotateLeft32(Node node, long x, long k) {
        return Integer.rotateLeft((int) x, (int) k) & 0xFFFFFFFFL;
    }

    static long math_bits_ReverseBytes64(Node node, long x) {
        return Long.reverseBytes(x);
    }

    static long math_bits_ReverseBytes32(Node node, long x) {
        return Integer.reverseBytes((int) x) & 0xFFFFFFFFL;
    }
}
