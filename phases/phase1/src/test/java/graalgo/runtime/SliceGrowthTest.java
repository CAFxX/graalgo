package graalgo.runtime;

// verifies: GG-0020 (WS G: slice growth policy)
// M7: pins Intrinsics.nextSliceCap to the Go runtime algorithm
// (runtime/slice.go nextslicecap + roundupsize, go1.27.1). Values were
// computed from the Go source by hand and verified against the -N -l
// (unoptimized) gc behavior, which uses the pure runtime path (no
// growsliceBuf stack-buffer optimization).
public final class SliceGrowthTest {
    public void testNextSliceCap() {
        // (newLen, oldCap, elemSize, hasPointers) -> expected newCap
        // int (8 bytes, no pointers)
        assertCap(1, 0, 8, false, 1);
        assertCap(2, 1, 8, false, 2);
        assertCap(3, 2, 8, false, 4);
        assertCap(4, 4, 8, false, 8);
        assertCap(5, 4, 8, false, 8);
        assertCap(9, 8, 8, false, 16);
        assertCap(17, 16, 8, false, 32);
        // byte (1 byte, no pointers): size-class rounding matters.
        // nextslicecap(1,0)=1, but roundupsize(1)=8 -> cap 8.
        assertCap(1, 0, 1, false, 8);
        assertCap(3, 2, 1, false, 4);
        assertCap(5, 4, 1, false, 8);
        assertCap(9, 8, 1, false, 16);
        // large: 1.25x growth after threshold
        // oldCap=256, newLen=257 -> 256 + (256+768)/4 = 256+256=512
        assertCap(257, 256, 8, false, 512);
    }

    private static void assertCap(int newLen, int oldCap, long elemSize,
            boolean hasPointers, int expected) {
        int actual = Intrinsics.nextSliceCap(newLen, oldCap, elemSize, hasPointers);
        if (actual != expected) {
            throw new AssertionError("nextSliceCap(" + newLen + ", " + oldCap
                    + ", " + elemSize + ") = " + actual + ", expected " + expected);
        }
    }
}
