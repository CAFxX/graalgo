package graalgo.runtime;

import graalgo.frontend.FrontendException;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;
import java.util.Arrays;
import java.util.List;

/**
 * structSizeBytes nested-struct/array support: computing the Go ABI size of
 * a struct whose fields are themselves structs or arrays used to throw
 * {@code FrontendException("... non-basic kind STRUCT ...")}, which broke
 * building any package whose transitive dependencies contain such a type
 * (e.g. reflect's struct with a nested-struct "Key" field, reached via
 * {@code import "fmt"}).
 *
 * <p>Each positive test would have thrown before the fix (demonstrably
 * capable of failing); the negative tests pin the remaining fail-fast
 * behavior for shapes the descriptor cannot represent.
 */
public final class StructSizeNestedTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void checkEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    /** 7-arg layout helper: structDescs[i] is the nested descriptor for STRUCT-kind fields. */
    private static GoStructDescriptor layout7(String name, List<String> names, List<GoKind> kinds,
            List<Integer> lens, List<GoKind> elemKinds,
            List<GoStructDescriptor> elemDescs, List<GoStructDescriptor> structDescs) {
        return GoStructDescriptor.layout(name, names, kinds, lens, elemKinds, elemDescs, structDescs);
    }

    private static List<Integer> noLens(int n) {
        return java.util.Collections.nCopies(n, -1);
    }

    private static <T> List<T> noDescs(int n) {
        return java.util.Collections.nCopies(n, null);
    }

    /** struct { x int32; y int8 } → x@0..4, y@4..5, padded to 8 (align 4). */
    private static GoStructDescriptor innerXY() {
        return GoStructDescriptor.layout("Inner",
                List.of("x", "y"), List.of(GoKind.INT32, GoKind.INT8));
    }

    /** Nested struct field: struct { a int8; b struct{x int32; y int8} } → 12 bytes. */
    public void testNestedStructSize() {
        GoStructDescriptor inner = innerXY();
        checkEq(8, Intrinsics.structSizeBytes(inner), "inner size");
        GoStructDescriptor outer = layout7("Outer",
                List.of("a", "b"), List.of(GoKind.INT8, GoKind.STRUCT),
                noLens(2), noDescs(2), noDescs(2), Arrays.asList(null, inner));
        // a@0..1, pad to 4, b@4..12 → size 12, align 4.
        checkEq(12, Intrinsics.structSizeBytes(outer), "outer size");
    }

    /** Doubly-nested structs recurse: 24 bytes. */
    public void testDoublyNestedStructSize() {
        GoStructDescriptor l3 = GoStructDescriptor.layout("L3",
                List.of("p"), List.of(GoKind.INT64));
        checkEq(8, Intrinsics.structSizeBytes(l3), "l3 size");
        GoStructDescriptor l2 = layout7("L2",
                List.of("q", "r"), List.of(GoKind.INT8, GoKind.STRUCT),
                noLens(2), noDescs(2), noDescs(2), Arrays.asList(null, l3));
        // q@0..1, pad to 8, r@8..16 → size 16, align 8.
        checkEq(16, Intrinsics.structSizeBytes(l2), "l2 size");
        GoStructDescriptor l1 = layout7("L1",
                List.of("s", "t"), List.of(GoKind.INT16, GoKind.STRUCT),
                noLens(2), noDescs(2), noDescs(2), Arrays.asList(null, l2));
        // s@0..2, pad to 8, t@8..24 → size 24, align 8.
        checkEq(24, Intrinsics.structSizeBytes(l1), "l1 size");
    }

    /** Array field: struct { arr [4]int16 } → 4*2 = 8 bytes, align 2. */
    public void testArrayFieldSize() {
        GoStructDescriptor d = layout7("A",
                List.of("arr"), List.of(GoKind.ARRAY),
                List.of(4), Arrays.asList(GoKind.INT16), noDescs(1), noDescs(1));
        checkEq(8, Intrinsics.structSizeBytes(d), "array field size");
    }

    /** Array field alignment follows the element: struct { c int8; arr [3]int32 } → 16. */
    public void testArrayFieldAlignment() {
        GoStructDescriptor d = layout7("A",
                List.of("c", "arr"), List.of(GoKind.INT8, GoKind.ARRAY),
                Arrays.asList(-1, 3), Arrays.asList(null, GoKind.INT32),
                noDescs(2), noDescs(2));
        // c@0..1, pad to 4, arr@4..16 → size 16, align 4.
        checkEq(16, Intrinsics.structSizeBytes(d), "array field align");
    }

    /** Array of structs: struct { items [3]struct{x int32; y int8} } → 3*8 = 24. */
    public void testArrayOfStructFieldSize() {
        GoStructDescriptor inner = innerXY();
        GoStructDescriptor d = layout7("B",
                List.of("items"), List.of(GoKind.ARRAY),
                List.of(3), Arrays.asList(GoKind.STRUCT), Arrays.asList(inner), noDescs(1));
        checkEq(24, Intrinsics.structSizeBytes(d), "array-of-struct size");
    }

    /** Flat structs are unchanged: struct { a int8; b int32 } → 8. */
    public void testFlatStructUnchanged() {
        GoStructDescriptor d = GoStructDescriptor.layout("Flat",
                List.of("a", "b"), List.of(GoKind.INT8, GoKind.INT32));
        checkEq(8, Intrinsics.structSizeBytes(d), "flat size");
    }

    /** STRUCT field without a nested descriptor still fails fast (was the old behavior for all). */
    public void testStructFieldWithoutDescriptorFails() {
        GoStructDescriptor d = GoStructDescriptor.layout("NoDesc",
                List.of("a", "b"), List.of(GoKind.INT8, GoKind.STRUCT));
        boolean threw = false;
        try {
            Intrinsics.structSizeBytes(d);
        } catch (FrontendException e) {
            threw = true;
        }
        check(threw, "expected FrontendException for STRUCT field without descriptor");
    }

    /** ARRAY-of-ARRAY elements are not representable in the descriptor → fail fast. */
    public void testNestedArrayElementFails() {
        GoStructDescriptor d = layout7("N",
                List.of("m"), List.of(GoKind.ARRAY),
                List.of(2), Arrays.asList(GoKind.ARRAY), noDescs(1), noDescs(1));
        boolean threw = false;
        try {
            Intrinsics.structSizeBytes(d);
        } catch (FrontendException e) {
            threw = true;
        }
        check(threw, "expected FrontendException for ARRAY-of-ARRAY element");
    }
}
