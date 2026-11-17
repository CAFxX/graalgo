package graalgo.p3reflect;

import graalgo.values.GoBox;
import graalgo.values.GoByteCell;

/**
 * fix-redmaster-r59: unit tests for {@link GoByteCell} — the byte cell
 * over a Long-carried scalar field, minted for sub-word reads such as
 * {@code *(*ctrl)(unsafe.Add(unsafe.Pointer(g), k))} over a group
 * image's ctrl word.
 *
 * <p>Falsifiability: every byte read pins an exact value of a known
 * word; every write pins the exact merged word (a wrong byte index,
 * endianness, or mask fails). Negative coverage: out-of-range byte
 * index, null wrapped cell, and non-Long storage all fail loudly.
 */
public final class GoByteCellTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Word 0x0807060504030201: byte k (little-endian) is k+1. */
    private static GoBox wordBox() {
        return new GoBox(Long.valueOf(0x0807060504030201L));
    }

    public void testReadAllBytes() {
        GoBox box = wordBox();
        for (int k = 0; k < 8; k++) {
            Object v = new GoByteCell(box, k).get();
            assertTrue(v instanceof Long, "byte read yields a Long");
            assertEquals(k + 1, ((Long) v).longValue(), "byte " + k);
        }
    }

    public void testWriteMergesOnlyThatByte() {
        GoBox box = wordBox();
        new GoByteCell(box, 3).set(Long.valueOf(0xFF));
        assertEquals(0x08070605FF030201L, ((Long) box.get()).longValue(),
                "writing byte 3 must change only byte 3");
        new GoByteCell(box, 0).set(Long.valueOf(0x00));
        assertEquals(0x08070605FF030200L, ((Long) box.get()).longValue(),
                "writing byte 0 must change only byte 0");
        // The other bytes still read back through their cells.
        assertEquals(0x05, ((Long) new GoByteCell(box, 4).get()).longValue(), "byte 4 intact");
    }

    public void testSameStorage() {
        GoBox box = wordBox();
        GoByteCell a = new GoByteCell(box, 2);
        GoByteCell b = new GoByteCell(box, 2);
        GoByteCell c = new GoByteCell(box, 3);
        GoByteCell d = new GoByteCell(new GoBox(Long.valueOf(0x0807060504030201L)), 2);
        assertTrue(a.sameStorageAs(b), "same cell + index is the same storage");
        assertTrue(!a.sameStorageAs(c), "different index is different storage");
        assertTrue(!a.sameStorageAs(d), "different cell is different storage");
    }

    public void testBadIndexFailsLoud() {
        GoBox box = wordBox();
        try {
            new GoByteCell(box, 8);
            throw new AssertionError("byte index 8 must fail");
        } catch (IllegalArgumentException expected) {
            // expected
        }
        try {
            new GoByteCell(box, -1);
            throw new AssertionError("byte index -1 must fail");
        } catch (IllegalArgumentException expected) {
            // expected
        }
        try {
            new GoByteCell(null, 0);
            throw new AssertionError("null cell must fail");
        } catch (IllegalArgumentException expected) {
            // expected
        }
    }

    public void testNonLongStorageFailsLoud() {
        GoBox box = new GoBox("not a long");
        try {
            new GoByteCell(box, 0).get();
            throw new AssertionError("reading a non-Long cell must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
        try {
            new GoByteCell(box, 0).set(Long.valueOf(1));
            throw new AssertionError("writing a non-Long cell must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
    }
}
