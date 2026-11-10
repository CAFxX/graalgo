package graalgo.runtime;

import java.util.List;

import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

// verifies: GG-0020 (WS G: slice growth policy)
// M7: pins Intrinsics.nextSliceCap to the Go runtime algorithm
// (runtime/slice.go nextslicecap + roundupsize, go1.27.1). Values were
// computed from the Go source by hand and verified against the -N -l
// (unoptimized) gc behavior, which uses the pure runtime path (no
// growsliceBuf stack-buffer optimization).
public final class SliceGrowthTest {

    /** Descriptor for struct{F int64}: one long field at index 0. */
    private static GoStructDescriptor structDesc() {
        return new GoStructDescriptor("S", List.of(
                new GoStructDescriptor.Field("F", GoKind.INT64, true, 0, -1, null, null, null)));
    }

    private static GoStruct structWith(long f) {
        return new GoStruct(structDesc(), new long[]{f}, new Object[0]);
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /**
     * R22-F-3: growslice must deep-copy struct elements — after a growing
     * append the old and new slices must not share element objects, or
     * {@code t[0].F = 1} observably mutates {@code s[0].F}.
     * RED (pre-fix): the grown backing holds the same GoStruct objects.
     */
    public void testGrowsliceStructElementsCopied() {
        GoStruct elem = structWith(1);
        GoSlice old = new GoSlice(new Object[]{elem}, 0, 1, 1);
        GoSlice grown = Intrinsics.growslice(old, GoKind.STRUCT, structDesc(), 2, 8, false);
        Object[] arr = (Object[]) grown.array();
        assertTrue(arr[0] != elem,
                "growslice shared the struct element object (R22-F-3)");
        assertTrue(arr[0] instanceof GoStruct gs && gs.getLong(0) == 1,
                "growslice copied element has wrong value");
        ((GoStruct) arr[0]).setLong(0, 99);
        assertTrue(elem.getLong(0) == 1,
                "mutating the grown element mutated the original (R22-F-3): F="
                        + elem.getLong(0));
    }

    /**
     * R22-F-3: growslice must deep-copy nested structs too (a struct field
     * holding a struct must not alias across the copy).
     */
    public void testGrowsliceNestedStructDeepCopied() {
        GoStructDescriptor innerDesc = structDesc();
        GoStructDescriptor outerDesc = new GoStructDescriptor("O", List.of(
                new GoStructDescriptor.Field("In", GoKind.STRUCT, false, 0, -1, null, null, innerDesc)));
        GoStruct inner = structWith(7);
        GoStruct outer = new GoStruct(outerDesc, new long[0], new Object[]{inner});
        GoSlice old = new GoSlice(new Object[]{outer}, 0, 1, 1);
        GoSlice grown = Intrinsics.growslice(old, GoKind.STRUCT, outerDesc, 2, 8, false);
        GoStruct copy = (GoStruct) ((Object[]) grown.array())[0];
        assertTrue(copy != outer, "growslice shared the outer struct (R22-F-3)");
        assertTrue(copy.getObject(0) != inner,
                "growslice shared the nested struct (R22-F-3)");
        ((GoStruct) copy.getObject(0)).setLong(0, 99);
        assertTrue(inner.getLong(0) == 7,
                "mutating the grown nested struct mutated the original (R22-F-3)");
    }

    /**
     * R22-F-3: growslice must deep-copy array elements (a [N]T element is
     * a value; its inner array must not alias across the copy).
     */
    public void testGrowsliceArrayElementsCopied() {
        int[] inner = new int[]{1, 2};
        GoSlice old = new GoSlice(new Object[]{inner}, 0, 1, 1);
        GoSlice grown = Intrinsics.growslice(old, GoKind.ARRAY, null, 2, 8, false);
        Object[] arr = (Object[]) grown.array();
        assertTrue(arr[0] != inner,
                "growslice shared the array element object (R22-F-3)");
        ((int[]) arr[0])[0] = 99;
        assertTrue(inner[0] == 1,
                "mutating the grown array element mutated the original (R22-F-3)");
    }

    /**
     * R22-F-3: slicecopy (the {@code copy} builtin) must deep-copy struct
     * elements — {@code dst[i].F = 1} must not mutate {@code src[i].F}.
     * RED (pre-fix): the typed-memmove default branch raw-copies refs.
     */
    public void testSlicecopyStructElementsCopied() {
        GoStruct srcElem = structWith(1);
        GoSlice src = new GoSlice(new Object[]{srcElem}, 0, 1, 1);
        GoStruct dstElem = structWith(0);
        GoSlice dst = new GoSlice(new Object[]{dstElem}, 0, 1, 1);
        int n = Intrinsics.slicecopy(dst, src, GoKind.STRUCT);
        assertTrue(n == 1, "slicecopy copied " + n + " elements, expected 1");
        Object[] arr = (Object[]) dst.array();
        assertTrue(arr[0] != srcElem,
                "slicecopy shared the struct element object (R22-F-3)");
        ((GoStruct) arr[0]).setLong(0, 99);
        assertTrue(srcElem.getLong(0) == 1,
                "mutating the copy mutated the source (R22-F-3): F=" + srcElem.getLong(0));
    }

    /**
     * R22-F-3: overlapping slicecopy of structs keeps gc's memmove values
     * AND detaches: {@code copy(s[1:], s[:2])} must hold the original
     * values, and mutating the destination must not touch the source slots.
     */
    public void testSlicecopyOverlapStructKeepsValues() {
        GoStruct a = structWith(1);
        GoStruct b = structWith(2);
        Object[] backing = new Object[]{a, b, structWith(0)};
        GoSlice dst = new GoSlice(backing, 1, 2, 2);
        GoSlice src = new GoSlice(backing, 0, 2, 2);
        int n = Intrinsics.slicecopy(dst, src, GoKind.STRUCT);
        assertTrue(n == 2, "overlapping slicecopy copied " + n + ", expected 2");
        GoStruct d0 = (GoStruct) backing[1];
        GoStruct d1 = (GoStruct) backing[2];
        assertTrue(d0.getLong(0) == 1 && d1.getLong(0) == 2,
                "overlapping slicecopy has wrong values: " + d0.getLong(0) + "," + d1.getLong(0));
        assertTrue(d0 != a && d1 != b,
                "overlapping slicecopy shared element objects (R22-F-3)");
        d0.setLong(0, 99);
        assertTrue(a.getLong(0) == 1,
                "mutating the overlapped copy mutated the source (R22-F-3)");
    }

    /**
     * Negative control (gc semantics): copying a {@code []any} slice copies
     * interface headers only — the struct data inside stays shared. Pins
     * the header-only rule so the fix does not over-copy interface data.
     */
    public void testSlicecopyInterfaceElementsShareData() {
        GoStruct elem = structWith(1);
        graalgo.values.GoInterface boxed = new graalgo.values.GoInterface(
                new graalgo.values.GoTypeDescriptor("main.S", GoKind.STRUCT, false), elem, null);
        GoSlice src = new GoSlice(new Object[]{boxed}, 0, 1, 1);
        GoSlice dst = new GoSlice(new Object[]{null}, 0, 1, 1);
        int n = Intrinsics.slicecopy(dst, src, GoKind.INTERFACE);
        assertTrue(n == 1, "slicecopy copied " + n + ", expected 1");
        graalgo.values.GoInterface got = (graalgo.values.GoInterface) ((Object[]) dst.array())[0];
        assertTrue(got.data() == elem,
                "[]any slicecopy must share the interface data (gc header copy)");
    }

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
        // nextslicecap(3,2)=4, roundupsize(4)=8 -> cap 8.
        assertCap(1, 0, 1, false, 8);
        assertCap(3, 2, 1, false, 8);
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
