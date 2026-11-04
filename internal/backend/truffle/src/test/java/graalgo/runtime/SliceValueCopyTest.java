package graalgo.runtime;

import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.List;

// Slice value-copy: growslice/slicecopy must deep-copy struct and array
// elements (Go value semantics) while still sharing pointer and interface
// elements (real Go aliasing). The corpus differential tests bi23-bi27
// cover the same properties end to end through append/copy; these unit
// tests pin the Intrinsics layer directly, including the overlapping
// copy direction that only shows up inside one backing array.
public final class SliceValueCopyTest {
    /** Descriptor for struct{ F int }: F lives in the longs carrier. */
    private static GoStructDescriptor desc() {
        return GoStructDescriptor.layout("P", List.of("F"), List.of(GoKind.INT64));
    }

    private static GoStruct structVal(long f) {
        GoStruct s = GoStruct.zero(desc());
        s.setLong(0, f);
        return s;
    }

    private static long field(GoStruct s) {
        return s.getLong(0);
    }

    public void testSlicecopyStructIndependence() {
        Object[] srcArr = new Object[]{structVal(1), structVal(2)};
        Object[] dstArr = new Object[2];
        GoSlice src = new GoSlice(srcArr, 0, 2, 2);
        GoSlice dst = new GoSlice(dstArr, 0, 2, 2);
        int n = Intrinsics.slicecopy(dst, src, GoKind.STRUCT);
        assertEq(2, n, "slicecopy count");
        assertTrue(dstArr[0] != srcArr[0], "struct element must be copied, not shared");
        assertTrue(dstArr[1] != srcArr[1], "struct element must be copied, not shared");
        ((GoStruct) dstArr[0]).setLong(0, 9);
        assertEq(1, field((GoStruct) srcArr[0]), "mutating dst must not affect src");
        assertEq(9, field((GoStruct) dstArr[0]), "dst mutation visible in dst");
    }

    public void testSlicecopyOverlapBackward() {
        // copy(s[1:], s) on one backing array: dst starts after src, so a
        // forward loop would read the just-written value for the second
        // element (arr[2] would become 1, not 2).
        Object[] arr = new Object[]{structVal(1), structVal(2), structVal(3)};
        GoSlice src = new GoSlice(arr, 0, 3, 3);
        GoSlice dst = new GoSlice(arr, 1, 2, 2);
        int n = Intrinsics.slicecopy(dst, src, GoKind.STRUCT);
        assertEq(2, n, "overlap slicecopy count");
        assertEq(1, field((GoStruct) arr[0]), "untouched head element");
        assertEq(1, field((GoStruct) arr[1]), "first copied element");
        assertEq(2, field((GoStruct) arr[2]), "second copied element keeps old value");
        ((GoStruct) arr[1]).setLong(0, 9);
        assertEq(1, field((GoStruct) arr[0]), "overlap copy must not alias head");
        assertEq(2, field((GoStruct) arr[2]), "overlap copy must not alias tail");
    }

    public void testSlicecopyArrayElementIndependence() {
        Object[] srcArr = new Object[]{new int[]{1, 2}, new int[]{3, 4}};
        Object[] dstArr = new Object[2];
        GoSlice src = new GoSlice(srcArr, 0, 2, 2);
        GoSlice dst = new GoSlice(dstArr, 0, 2, 2);
        int n = Intrinsics.slicecopy(dst, src, GoKind.ARRAY);
        assertEq(2, n, "array slicecopy count");
        assertTrue(dstArr[0] != srcArr[0], "array element must be copied, not shared");
        ((int[]) dstArr[0])[0] = 9;
        assertEq(1, ((int[]) srcArr[0])[0], "mutating dst array must not affect src");
    }

    public void testSlicecopyPointerSharing() {
        // Negative control: pointer elements ARE shared by design.
        Object p = new Object();
        Object[] srcArr = new Object[]{p};
        Object[] dstArr = new Object[1];
        Intrinsics.slicecopy(new GoSlice(dstArr, 0, 1, 1),
                new GoSlice(srcArr, 0, 1, 1), GoKind.POINTER);
        assertTrue(dstArr[0] == p, "pointer element must stay shared");
    }

    public void testSlicecopyInterfaceSharing() {
        // Negative control: interface elements share the (type, data) pair.
        GoInterface v = new GoInterface("desc", 42L, "itab");
        Object[] srcArr = new Object[]{v};
        Object[] dstArr = new Object[1];
        Intrinsics.slicecopy(new GoSlice(dstArr, 0, 1, 1),
                new GoSlice(srcArr, 0, 1, 1), GoKind.INTERFACE);
        assertTrue(dstArr[0] == v, "interface element must stay shared");
    }

    public void testGrowsliceStructIndependence() {
        Object[] oldArr = new Object[]{structVal(1)};
        GoSlice old = new GoSlice(oldArr, 0, 1, 1);
        GoSlice grown = Intrinsics.growslice(old, GoKind.STRUCT, desc(), 2, 8, false);
        assertEq(2, grown.len(), "grown len");
        Object[] grownArr = (Object[]) grown.array();
        assertTrue(grownArr[grown.offset()] != oldArr[0],
                "grown element must be copied, not shared");
        ((GoStruct) grownArr[grown.offset()]).setLong(0, 9);
        assertEq(1, field((GoStruct) oldArr[0]), "mutating grown must not affect old");
    }

    public void testCopyValueNull() {
        assertTrue(GoStruct.copyValue(GoKind.STRUCT, null) == null, "null struct stays null");
        assertTrue(GoStruct.copyValue(GoKind.ARRAY, null) == null, "null array stays null");
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError("expected true: " + what);
        }
    }

    private static void assertEq(long expected, long actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + ", got " + actual);
        }
    }
}
