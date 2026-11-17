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

    /** r64 (C1): an Integer cell is NOT Long storage — bytes 4-7 would read sign-extension bytes. */
    public void testIntegerCellFailsLoud() {
        GoBox box = new GoBox(Integer.valueOf(0x01020304));
        try {
            new GoByteCell(box, 0).get();
            throw new AssertionError("reading an Integer cell must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
        try {
            new GoByteCell(box, 0).set(Long.valueOf(1));
            throw new AssertionError("writing an Integer cell must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
    }

    /** r64 (C1): a Double cell is NOT Long storage — longValue() would truncate 1.5 to 1. */
    public void testDoubleCellFailsLoud() {
        GoBox box = new GoBox(Double.valueOf(1.5));
        try {
            new GoByteCell(box, 0).get();
            throw new AssertionError("reading a Double cell must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
        try {
            new GoByteCell(box, 0).set(Long.valueOf(1));
            throw new AssertionError("writing a Double cell must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
    }

    /** r64 (C1): a floating-point byte value would truncate silently — it fails loudly. */
    public void testDoubleValueFailsLoud() {
        GoBox box = wordBox();
        try {
            new GoByteCell(box, 0).set(Double.valueOf(1.5));
            throw new AssertionError("storing a Double byte value must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
        try {
            new GoByteCell(box, 0).set(Float.valueOf(2.5f));
            throw new AssertionError("storing a Float byte value must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
    }

    /** r64 (C1): a guest uint8 arrives Integer-boxed — storing it is exact, not an error. */
    public void testIntegerValueStoresByte() {
        GoBox box = wordBox();
        new GoByteCell(box, 3).set(Integer.valueOf(0xAB));
        assertEquals(0x08070605AB030201L, ((Long) box.get()).longValue(),
                "writing byte 3 as Integer must change only byte 3");
        new GoByteCell(box, 0).set(Byte.valueOf((byte) 0x7F));
        assertEquals(0x08070605AB03027FL, ((Long) box.get()).longValue(),
                "writing byte 0 as Byte must change only byte 0");
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

    // ---- r69 (C1/F1): width-aware cells over sub-word integral storage ----

    /** A uint32 0x11223344 in an Integer box: bytes 0-3 are 44/33/22/11. */
    public void testIntegerStorageReadsBytes() {
        GoBox box = new GoBox(Integer.valueOf(0x11223344));
        assertEquals(0x44, ((Long) new GoByteCell(box, 0, 4).get()).longValue(), "byte 0");
        assertEquals(0x33, ((Long) new GoByteCell(box, 1, 4).get()).longValue(), "byte 1");
        assertEquals(0x22, ((Long) new GoByteCell(box, 2, 4).get()).longValue(), "byte 2");
        assertEquals(0x11, ((Long) new GoByteCell(box, 3, 4).get()).longValue(), "byte 3");
        // Byte 4 is past the 4-byte storage: the constructor refuses it.
        try {
            new GoByteCell(box, 4, 4);
            throw new AssertionError("byte index 4 of a width-4 cell must fail");
        } catch (IllegalArgumentException expected) {
            // expected
        }
        try {
            new GoByteCell(box, 0, 0);
            throw new AssertionError("width 0 must fail");
        } catch (IllegalArgumentException expected) {
            // expected
        }
        try {
            new GoByteCell(box, 0, 9);
            throw new AssertionError("width 9 must fail");
        } catch (IllegalArgumentException expected) {
            // expected
        }
    }

    /** Writes merge one byte and store back an Integer (box type preserved). */
    public void testIntegerStorageWritesMerge() {
        GoBox box = new GoBox(Integer.valueOf(0x11223344));
        new GoByteCell(box, 1, 4).set(Long.valueOf(0x99));
        assertTrue(box.get() instanceof Integer, "write-back stays Integer-boxed");
        assertEquals(0x11229944L, ((Integer) box.get()).longValue(), "byte 1 merged");
        new GoByteCell(box, 3, 4).set(Integer.valueOf(0xAA));
        assertTrue(box.get() instanceof Integer, "write-back stays Integer-boxed");
        // 0xAA229944 is negative as a signed int; compare int bits, not longValue().
        assertTrue(((Integer) box.get()).intValue() == (int) 0xAA229944, "byte 3 merged");
        // The other bytes still read back through their cells.
        assertEquals(0x99, ((Long) new GoByteCell(box, 1, 4).get()).longValue(), "byte 1 intact");
    }

    /** A Short box (width 2): bytes 0-1 exact, write-back stays Short. */
    public void testShortStorageReadsWrites() {
        GoBox box = new GoBox(Short.valueOf((short) 0x1122));
        assertEquals(0x22, ((Long) new GoByteCell(box, 0, 2).get()).longValue(), "byte 0");
        assertEquals(0x11, ((Long) new GoByteCell(box, 1, 2).get()).longValue(), "byte 1");
        new GoByteCell(box, 0, 2).set(Long.valueOf(0x33));
        assertTrue(box.get() instanceof Short, "write-back stays Short-boxed");
        assertEquals(0x1133L, ((Short) box.get()).longValue(), "byte 0 merged");
    }

    /** A Byte box (width 1): byte 0 exact; byte 1 is past the storage. */
    public void testByteStorageReadsWrites() {
        GoBox box = new GoBox(Byte.valueOf((byte) 0xAB));
        assertEquals(0xAB, ((Long) new GoByteCell(box, 0, 1).get()).longValue(), "byte 0");
        new GoByteCell(box, 0, 1).set(Long.valueOf(0x7F));
        assertTrue(box.get() instanceof Byte, "write-back stays Byte-boxed");
        assertEquals(0x7F, ((Byte) box.get()).longValue(), "byte 0 merged");
        try {
            new GoByteCell(box, 1, 1);
            throw new AssertionError("byte index 1 of a width-1 cell must fail");
        } catch (IllegalArgumentException expected) {
            // expected
        }
    }

    /** A box narrower than the declared width fails loudly (no extension bytes). */
    public void testNarrowerBoxFailsLoud() {
        GoBox intBox = new GoBox(Integer.valueOf(0x11223344));
        try {
            new GoByteCell(intBox, 0, 8).get();
            throw new AssertionError("width-8 cell over an Integer must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
        try {
            new GoByteCell(intBox, 0, 8).set(Long.valueOf(1));
            throw new AssertionError("width-8 cell over an Integer must fail on write");
        } catch (IllegalStateException expected) {
            // expected
        }
        GoBox shortBox = new GoBox(Short.valueOf((short) 0x1122));
        try {
            new GoByteCell(shortBox, 0, 4).get();
            throw new AssertionError("width-4 cell over a Short must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
    }

    /** Non-integral storage fails loudly under the width-aware cell too. */
    public void testWidthAwareNonIntegralFailsLoud() {
        GoBox doubleBox = new GoBox(Double.valueOf(1.5));
        try {
            new GoByteCell(doubleBox, 0, 4).get();
            throw new AssertionError("width-4 cell over a Double must fail");
        } catch (IllegalStateException expected) {
            // expected
        }
        GoBox stringBox = new GoBox("not a number");
        try {
            new GoByteCell(stringBox, 0, 4).set(Long.valueOf(1));
            throw new AssertionError("width-4 cell over a String must fail on write");
        } catch (IllegalStateException expected) {
            // expected
        }
    }

    /** A negative int32 reads its low bytes exactly (sign extension is masked out). */
    public void testNegativeIntegerReadsLowBytes() {
        GoBox box = new GoBox(Integer.valueOf(-1));
        for (int k = 0; k < 4; k++) {
            assertEquals(0xFF, ((Long) new GoByteCell(box, k, 4).get()).longValue(), "byte " + k);
        }
        GoBox box2 = new GoBox(Integer.valueOf(0x80000000));
        assertEquals(0x00, ((Long) new GoByteCell(box2, 0, 4).get()).longValue(), "byte 0");
        assertEquals(0x00, ((Long) new GoByteCell(box2, 1, 4).get()).longValue(), "byte 1");
        assertEquals(0x00, ((Long) new GoByteCell(box2, 2, 4).get()).longValue(), "byte 2");
        assertEquals(0x80, ((Long) new GoByteCell(box2, 3, 4).get()).longValue(), "byte 3");
    }
}
