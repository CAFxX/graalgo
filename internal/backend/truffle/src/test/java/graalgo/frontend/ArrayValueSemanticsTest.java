package graalgo.frontend;

import graalgo.values.GoKind;

/**
 * Regression test for bug-038 (2026-10-15): Go arrays are value types —
 * a function receiving an array argument must get a copy, so callee
 * mutation ({@code a[0] = 99}) does not leak to the caller.
 *
 * <p>The mechanism is {@link Boxing#copyForAssign}: call sites
 * ({@code FuncValueCallNode}, {@code DeferNode}) must wrap argument boxing
 * with it. This test pins the copy semantics themselves; the call-site
 * wiring is pinned by the Go-level repro in
 * {@code hidden_files/adversarial/bugs/bug-038/repro.go}.
 *
 * <p>RED (pre-fix): if {@code copyForAssign} returned the array by
 * reference, {@code testArrayArgIsCopied} fails because mutating the
 * "copy" would be visible through the original. GREEN: the copy is deep
 * and detached.
 */
public final class ArrayValueSemanticsTest {

    /**
     * Array arguments are deep-copied: mutating the copy does not affect
     * the original. This is what the call site relies on for Go value
     * semantics.
     */
    public void testArrayArgIsCopied() {
        int[] original = new int[]{1, 2, 3, 4};
        Object copy = Boxing.copyForAssign(GoKind.ARRAY, original);
        if (copy == original) {
            throw new AssertionError(
                    "copyForAssign(ARRAY) returned the same array object — "
                    + "callee mutation would leak to the caller (bug-038)");
        }
        int[] copyArr = (int[]) copy;
        copyArr[0] = 99;
        if (original[0] != 1) {
            throw new AssertionError(
                    "copyForAssign(ARRAY) copy is aliased: original[0] = "
                    + original[0] + ", expected 1 (bug-038)");
        }
        if (copyArr[3] != 4) {
            throw new AssertionError(
                    "copyForAssign(ARRAY) copy has wrong contents");
        }
    }

    /**
     * Nested arrays are deep-copied too: mutating an inner array of the
     * copy does not affect the original's inner array.
     */
    public void testNestedArrayArgIsDeepCopied() {
        int[][] original = new int[][]{{1, 2}, {3, 4}};
        Object copy = Boxing.copyForAssign(GoKind.ARRAY, original);
        int[][] copyArr = (int[][]) copy;
        if (copyArr[1] == original[1]) {
            throw new AssertionError(
                    "copyForAssign(ARRAY) inner array aliased (bug-038)");
        }
        copyArr[1][1] = 99;
        if (original[1][1] != 4) {
            throw new AssertionError(
                    "copyForAssign(ARRAY) nested copy aliased: original[1][1] = "
                    + original[1][1] + ", expected 4 (bug-038)");
        }
    }

    /**
     * Slices are reference types: copyForAssign must NOT copy them, or
     * slice mutation through a call would silently stop working.
     */
    public void testSliceArgIsNotCopied() {
        // GoSlice is the slice carrier; SLICE kind must pass through.
        Object slice = new graalgo.values.GoSlice(new int[]{1, 2, 3}, 0, 3, 3);
        Object result = Boxing.copyForAssign(GoKind.SLICE, slice);
        if (result != slice) {
            throw new AssertionError(
                    "copyForAssign(SLICE) copied the slice — slices are "
                    + "reference types and must stay shared");
        }
    }
}
