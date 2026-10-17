package graalgo.values;

import java.util.Arrays;
import java.util.List;

/**
 * Unit tests for the RV-P0 runtime-npe fix: {@link GoStruct#zero} must
 * recursively materialize nested STRUCT-kind fields (a Go struct value's
 * nested struct field is never absent), so that {@code &z.Nested} yields a
 * pointer to a real zero struct instead of a cell over Java null (which
 * made {@code SelectorWriteNode} NPE at {@code s.descriptor()}).
 *
 * <p>RED (pre-fix): {@code testNestedStructZeroRecursive},
 * {@code testCellPointerRoundTrip} and {@code testArrayOfStructZeroRecursive}
 * fail because the old {@code GoStruct.zero} left STRUCT-kind fields null;
 * {@code testCellPointerRoundTrip} is the JVM-level rendering of the §2
 * repro ({@code p := &a[0].In; p.X = 42} NPE'd).
 * GREEN: nested structs are materialized; opaque sync primitives and
 * null-descriptor fields stay null (the carve-out).
 */
public final class GoStructZeroTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError("want true: " + what);
        }
    }

    private static void assertNull(Object got, String what) {
        if (got != null) {
            throw new AssertionError(what + ": want null, got " + got);
        }
    }

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    /** Descriptor for {@code struct{ X int64 }}-shaped types. */
    private static GoStructDescriptor intStruct(String typeName) {
        return GoStructDescriptor.layout(typeName, List.of("X"), List.of(GoKind.INT64));
    }

    /** The §2 repro shape: {@code Outer{ In Inner }} with {@code Inner{ X int64 }}. */
    private static GoStructDescriptor outerDesc(GoStructDescriptor inner) {
        return GoStructDescriptor.layout("main.Outer",
                List.of("In"), List.of(GoKind.STRUCT),
                List.of(-1),
                Arrays.asList((GoKind) null),
                Arrays.asList((GoStructDescriptor) null),
                List.of(inner));
    }

    public void testNestedStructZeroRecursive() {
        GoStructDescriptor inner = intStruct("main.Inner");
        GoStruct z = GoStruct.zero(outerDesc(inner));
        Object in = z.getObject(0);
        assertTrue(in instanceof GoStruct, "nested STRUCT field materialized, got " + in);
        GoStruct inStruct = (GoStruct) in;
        assertTrue(inStruct.descriptor() == inner, "nested descriptor identity");
        assertEq(0L, inStruct.getLong(0), "nested X is zero");
    }

    public void testThreeLevelNesting() {
        GoStructDescriptor inner = intStruct("main.Inner");
        GoStructDescriptor mid = outerDesc(inner); // main.Outer{ In Inner }
        GoStructDescriptor top = GoStructDescriptor.layout("main.Top",
                List.of("M"), List.of(GoKind.STRUCT),
                List.of(-1),
                Arrays.asList((GoKind) null),
                Arrays.asList((GoStructDescriptor) null),
                List.of(mid));
        GoStruct z = GoStruct.zero(top);
        GoStruct m = (GoStruct) z.getObject(0);
        assertTrue(m != null, "level-2 materialized");
        GoStruct in = (GoStruct) m.getObject(0);
        assertTrue(in != null, "level-3 materialized");
        assertEq(0L, in.getLong(0), "leaf zero");
    }

    /**
     * JVM-level rendering of the §2 repro: {@code &a[0].In} is a
     * {@link GoPointer} over a field cell; pre-fix the cell held null and
     * {@code p.X = 42} NPE'd in {@code SelectorWriteNode}. Post-fix the
     * referent is a real zero struct and the write round-trips.
     */
    public void testCellPointerRoundTrip() {
        GoStructDescriptor inner = intStruct("main.Inner");
        GoStruct z = GoStruct.zero(outerDesc(inner));
        // Field cell over z.In, mirroring GoFieldCell (values-package only:
        // no frontend dependency in this test).
        GoCell cell = new GoCell() {
            @Override public Object get() { return z.getObject(0); }
            @Override public void set(Object value) { z.setObject(0, value); }
            @Override public boolean sameStorageAs(GoCell other) { return other == this; }
        };
        GoPointer p = new GoPointer(cell);
        assertTrue(!p.isNil(), "&z.In is a non-nil pointer");
        Object referent = p.referent();
        assertTrue(referent instanceof GoStruct,
                "cell referent is a materialized zero struct, got " + referent);
        // p.X = 42
        ((GoStruct) referent).setLong(0, 42L);
        assertEq(42L, ((GoStruct) z.getObject(0)).getLong(0),
                "write through the cell pointer is visible in the owner");
    }

    /**
     * ARRAY-of-STRUCT fields recurse per element: each element's nested
     * struct fields must be materialized too (pre-fix they stayed null
     * inside array elements).
     */
    public void testArrayOfStructZeroRecursive() {
        GoStructDescriptor inner = intStruct("main.Inner");
        GoStructDescriptor mid = outerDesc(inner); // Mid{ In Inner }
        GoStructDescriptor holder = GoStructDescriptor.layout("main.Holder",
                List.of("A"), List.of(GoKind.ARRAY),
                List.of(2),
                List.of(GoKind.STRUCT),
                List.of(mid),
                Arrays.asList((GoStructDescriptor) null));
        GoStruct z = GoStruct.zero(holder);
        Object[] arr = (Object[]) z.getObject(0);
        assertEq(2, arr.length, "array length");
        for (int i = 0; i < arr.length; i++) {
            assertTrue(arr[i] instanceof GoStruct, "element " + i + " zero-filled");
            Object in = ((GoStruct) arr[i]).getObject(0);
            assertTrue(in instanceof GoStruct,
                    "element " + i + " nested struct materialized, got " + in);
            assertEq(0L, ((GoStruct) in).getLong(0), "element " + i + " leaf zero");
        }
    }

    /**
     * Opaque-primitive carve-out: STRUCT fields whose nested descriptor
     * denotes {@code sync.Pool}, {@code sync.Map} or {@code sync/atomic.*}
     * stay null — the frontend template mechanism owns their live Java
     * carriers; fabricating an empty GoStruct would be a subtler wrong
     * value. Plain nested structs alongside are still materialized.
     */
    public void testOpaquePrimitiveCarveOut() {
        GoStructDescriptor poolDesc = intStruct("sync.Pool");
        GoStructDescriptor mapDesc = intStruct("sync.Map");
        GoStructDescriptor atomicDesc = intStruct("sync/atomic.Int64");
        GoStructDescriptor atomicPtrDesc = intStruct("sync/atomic.Pointer[main.T]");
        GoStructDescriptor inner = intStruct("main.Inner");
        GoStructDescriptor holder = GoStructDescriptor.layout("main.Holder",
                List.of("P", "M", "A", "AP", "In"),
                List.of(GoKind.STRUCT, GoKind.STRUCT, GoKind.STRUCT, GoKind.STRUCT, GoKind.STRUCT),
                Arrays.asList(-1, -1, -1, -1, -1),
                Arrays.asList((GoKind) null, (GoKind) null, (GoKind) null, (GoKind) null, (GoKind) null),
                Arrays.asList((GoStructDescriptor) null, (GoStructDescriptor) null,
                        (GoStructDescriptor) null, (GoStructDescriptor) null, (GoStructDescriptor) null),
                Arrays.asList(poolDesc, mapDesc, atomicDesc, atomicPtrDesc, inner));
        GoStruct z = GoStruct.zero(holder);
        assertNull(z.getObject(0), "sync.Pool field stays null");
        assertNull(z.getObject(1), "sync.Map field stays null");
        assertNull(z.getObject(2), "sync/atomic.Int64 field stays null");
        assertNull(z.getObject(3), "sync/atomic.Pointer[T] field stays null");
        assertTrue(z.getObject(4) instanceof GoStruct, "plain nested struct still materialized");
    }

    /** A STRUCT-kind field with no nested descriptor keeps legacy behavior (null). */
    public void testNullStructDescStaysNull() {
        GoStructDescriptor holder = GoStructDescriptor.layout("main.Holder",
                List.of("In"), List.of(GoKind.STRUCT),
                List.of(-1),
                Arrays.asList((GoKind) null),
                Arrays.asList((GoStructDescriptor) null),
                Arrays.asList((GoStructDescriptor) null));
        GoStruct z = GoStruct.zero(holder);
        assertNull(z.getObject(0), "STRUCT field without structDesc stays null");
    }

    /** Deep (finite) nesting terminates with correct leaf values. */
    public void testDeepNestingTerminates() {
        GoStructDescriptor leaf = intStruct("main.Leaf");
        GoStructDescriptor d = leaf;
        int depth = 60;
        for (int i = 0; i < depth; i++) {
            d = outerDesc(d);
        }
        GoStruct z = GoStruct.zero(d);
        GoStruct cur = z;
        for (int i = 0; i < depth; i++) {
            Object next = cur.getObject(0);
            assertTrue(next instanceof GoStruct, "nesting level " + i + " materialized");
            cur = (GoStruct) next;
        }
        assertEq(0L, cur.getLong(0), "deep leaf zero");
    }

    /**
     * Cycle guard: re-entering a descriptor that is already on the current
     * materialization path must fail loud ({@link IllegalStateException}),
     * never {@link StackOverflowError}. Go's type system forbids
     * value-recursive structs, so only hand-built descriptors can cycle;
     * records are immutable, so no public API can close a real reference
     * cycle on this JDK — instead this white-box test invokes the private
     * {@code zero(descriptor, visiting)} overload with the descriptor
     * pre-seeded on the visiting path, which is exactly the state a cyclic
     * graph would produce.
     */
    public void testCyclicDescriptorFailsLoud() throws Exception {
        GoStructDescriptor inner = intStruct("main.Inner");
        GoStructDescriptor outer = outerDesc(inner);
        java.util.IdentityHashMap<GoStructDescriptor, Boolean> visiting =
                new java.util.IdentityHashMap<>();
        visiting.put(outer, Boolean.TRUE); // simulate: outer already on the path
        java.lang.reflect.Method zero2 = GoStruct.class.getDeclaredMethod(
                "zero", GoStructDescriptor.class, java.util.IdentityHashMap.class);
        zero2.setAccessible(true);
        boolean threw = false;
        try {
            zero2.invoke(null, outer, visiting);
        } catch (java.lang.reflect.InvocationTargetException e) {
            assertTrue(e.getCause() instanceof IllegalStateException,
                    "guard fails loud, got " + e.getCause());
            threw = true;
        } catch (StackOverflowError soe) {
            throw new AssertionError(
                    "cyclic descriptor caused StackOverflowError, want IllegalStateException: " + soe);
        }
        assertTrue(threw, "re-entered descriptor throws IllegalStateException");
    }
}
