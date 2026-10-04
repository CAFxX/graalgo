import java.nio.file.*;
import java.util.*;

/**
 * BitsForm: the JVM-COVERS formulation of math/bits (amd64). Go's
 * math/bits ops are compiler intrinsics over BSR/BSF/POPCNT/ROR; on the
 * JVM they map exactly to java.lang.Long methods:
 *
 *   bits.LeadingZeros64  -> Long.numberOfLeadingZeros
 *   bits.TrailingZeros64 -> Long.numberOfTrailingZeros
 *   bits.OnesCount64     -> Long.bitCount
 *   bits.Reverse64       -> Long.reverse
 *   bits.ReverseBytes64  -> Long.reverseBytes
 *   bits.RotateLeft64    -> Long.rotateLeft
 *   bits.Len64           -> 64 - Long.numberOfLeadingZeros
 *
 * Reads hex uint64s from argv[0], prints one result line per input in the
 * same field order as bitsgo (the Go oracle). diff must be empty.
 */
public class BitsForm {
    public static void main(String[] a) throws Exception {
        for (String line : Files.readAllLines(Path.of(a[0]))) {
            long x = Long.parseUnsignedLong(line.trim(), 16);
            System.out.printf("%d %d %d %016x %016x %016x %d%n",
                Long.numberOfLeadingZeros(x),
                Long.numberOfTrailingZeros(x),
                Long.bitCount(x),
                Long.reverse(x),
                Long.reverseBytes(x),
                Long.rotateLeft(x, 17),
                64 - Long.numberOfLeadingZeros(x));
        }
    }
}
