package graalgo.p3reflect;

import graalgo.values.GoKind;

/**
 * fix-redmaster-r59: unit tests for {@link MapGroupLayout} — the gc
 * interleaved group geometry shared by the abi.MapType shell fill and
 * the header-view image builder.
 *
 * <p>Every expectation is hand-computed from gc's rules
 * ({@code cmd/compile/internal/reflectdata/map.go} {@code writeMapType} /
 * {@code MapGroupType} with {@code goexperiment.MapSplitGroup} off):
 * keysOff = slots field offset; keyStride = elemStride = slot size;
 * elemsOff = keysOff + elem offset in slot; elemOff = elem offset in
 * slot; indirect flags set iff the key/elem size exceeds 128. A wrong
 * slot-size or stride formula fails at least one case.
 */
public final class MapGroupLayoutTest {

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

    private static ReflectTypes.TypeInfo info(GoKind kind, long size, int align) {
        return new ReflectTypes.TypeInfo(0, kind, "", "", size, align,
                null, null, 0, null, null, null, null, null, false, null, "", -1);
    }

    private static void check(String label, ReflectTypes.TypeInfo key, ReflectTypes.TypeInfo elem,
            long groupSize, long keysOff, long keyStride, long elemsOff,
            long elemStride, long elemOff, int flags,
            boolean indirectKey, boolean indirectElem) {
        MapGroupLayout.Layout got = MapGroupLayout.of(key, elem);
        assertTrue(got != null, label + ": layout must not be null");
        assertEquals(groupSize, got.groupSize(), label + " groupSize");
        assertEquals(keysOff, got.keysOff(), label + " keysOff");
        assertEquals(keyStride, got.keyStride(), label + " keyStride");
        assertEquals(elemsOff, got.elemsOff(), label + " elemsOff");
        assertEquals(elemStride, got.elemStride(), label + " elemStride");
        assertEquals(elemOff, got.elemOff(), label + " elemOff");
        assertEquals(flags, got.flags(), label + " flags");
        assertTrue(got.indirectKey() == indirectKey, label + " indirectKey");
        assertTrue(got.indirectElem() == indirectElem, label + " indirectElem");
    }

    public void testStringInt() {
        // Slot {string(16); int(8)} = 24; group = 8 + 8*24 = 200.
        check("map[string]int", info(GoKind.STRING, 16, 8), info(GoKind.INT64, 8, 8),
                200, 8, 24, 24, 24, 16, 0, false, false);
    }

    public void testIntString() {
        // Slot {int(8); string(16)} = 24; group = 200; elem at slot+8.
        check("map[int]string", info(GoKind.INT64, 8, 8), info(GoKind.STRING, 16, 8),
                200, 8, 24, 16, 24, 8, 0, false, false);
    }

    public void testUint8Uint64() {
        // Slot {uint8(1); uint64(8)}: elem aligned to 8 -> slot 16; group = 8 + 8*16 = 136.
        check("map[uint8]uint64", info(GoKind.UINT8, 1, 1), info(GoKind.UINT64, 8, 8),
                136, 8, 16, 16, 16, 8, 0, false, false);
    }

    public void testIndirectKey() {
        // [200]byte key is indirect: slot field is a pointer (8,8);
        // slot {*key(8); int(8)} = 16; group = 136; flag 4.
        check("map[[200]byte]int", info(GoKind.ARRAY, 200, 1), info(GoKind.INT64, 8, 8),
                136, 8, 16, 16, 16, 8, MapGroupLayout.FLAG_INDIRECT_KEY, true, false);
    }

    public void testIndirectElem() {
        // [300]byte elem is indirect: slot {string(16); *elem(8)} = 24;
        // group = 200; flag 8.
        check("map[string][300]byte", info(GoKind.STRING, 16, 8), info(GoKind.ARRAY, 300, 1),
                200, 8, 24, 24, 24, 16, MapGroupLayout.FLAG_INDIRECT_ELEM, false, true);
    }

    public void testUnknownSizes() {
        ReflectTypes.TypeInfo str = info(GoKind.STRING, 16, 8);
        ReflectTypes.TypeInfo in = info(GoKind.INT64, 8, 8);
        assertTrue(MapGroupLayout.of(null, in) == null, "null key -> null layout");
        assertTrue(MapGroupLayout.of(str, null) == null, "null elem -> null layout");
        assertTrue(MapGroupLayout.of(info(GoKind.STRING, -1, 8), in) == null,
                "unknown key size -> null layout");
        assertTrue(MapGroupLayout.of(str, info(GoKind.INT64, -1, 8)) == null,
                "unknown elem size -> null layout");
    }
}
