package graalgo.values;

import java.util.List;

/**
 * Tests for {@link GoStruct#zero} recursive initialization
 * (P3-M16, fix-redmaster-r16).
 *
 * <p>Go's zero value is recursive: STRUCT-kind fields (including embedded
 * ones) must be zero structs, not Java null. A null embedded struct breaks
 * promoted field access (e.g. {@code l.shared} through embedded structs in
 * {@code sync.Pool}'s {@code []poolLocal}, which nil-deref panics).
 */
public final class GoStructZeroTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    /** Descriptor for a struct with the given field names/kinds and nested struct descriptors. */
    private static GoStructDescriptor desc(String name, List<String> fieldNames,
            List<GoKind> kinds, List<GoStructDescriptor> structDescs) {
        int n = fieldNames.size();
        java.util.List<Integer> lens = new java.util.ArrayList<>(n);
        java.util.List<GoKind> elemKinds = new java.util.ArrayList<>(n);
        java.util.List<GoStructDescriptor> elemDescs = new java.util.ArrayList<>(n);
        for (int i = 0; i < n; i++) {
            lens.add(-1);
            elemKinds.add(null);
            elemDescs.add(null);
        }
        return GoStructDescriptor.layout(name, fieldNames, kinds, lens, elemKinds, elemDescs, structDescs);
    }

    private static GoStructDescriptor intStruct(String name) {
        return desc(name, List.of("x"), List.of(GoKind.INT64),
                java.util.Collections.singletonList(null));
    }

    /**
     * A STRUCT-kind field with a descriptor zero-initializes to a recursive
     * zero struct, not Java null. Without the fix, the field is null and
     * promoted field selection through it nil-derefs.
     */
    public void testNestedStructZeroIsRecursive() {
        GoStructDescriptor inner = intStruct("inner");
        GoStructDescriptor outer = desc("outer",
                List.of("inner", "y"),
                List.of(GoKind.STRUCT, GoKind.INT64),
                java.util.Arrays.asList(inner, null));
        GoStruct z = GoStruct.zero(outer);
        Object nested = z.getObject(0);
        assertTrue(nested instanceof GoStruct, "nested STRUCT field is a GoStruct, not null");
        assertTrue(((GoStruct) nested).getLong(0) == 0L, "nested int field is zero");
        assertTrue(z.getLong(1) == 0L, "sibling int field is zero");
    }

    /**
     * Deep nesting terminates: Go struct types cannot directly contain
     * themselves by value, so the recursion is well-founded.
     */
    public void testDeepNestingTerminates() {
        GoStructDescriptor l3 = intStruct("l3");
        GoStructDescriptor l2 = desc("l2",
                List.of("c"), List.of(GoKind.STRUCT), List.of(l3));
        GoStructDescriptor l1 = desc("l1",
                List.of("c"), List.of(GoKind.STRUCT), List.of(l2));
        GoStruct z = GoStruct.zero(l1);
        Object c1 = z.getObject(0);
        assertTrue(c1 instanceof GoStruct, "level 1 nested is a GoStruct");
        Object c2 = ((GoStruct) c1).getObject(0);
        assertTrue(c2 instanceof GoStruct, "level 2 nested is a GoStruct");
        assertTrue(((GoStruct) c2).getLong(0) == 0L, "level 3 int field is zero");
    }

    /**
     * A STRUCT-kind field WITHOUT a descriptor keeps the legacy behavior
     * (null) gracefully — no crash, no infinite recursion.
     */
    public void testNullStructDescFallsBackToNull() {
        GoStructDescriptor outer = desc("outer",
                List.of("inner"), List.of(GoKind.STRUCT),
                java.util.Collections.singletonList(null));
        GoStruct z = GoStruct.zero(outer);
        assertTrue(z.getObject(0) == null, "STRUCT field without descriptor stays null");
    }
}
