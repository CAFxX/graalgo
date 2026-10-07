package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.source.Source;

import graalgo.iface.DescriptorNames;
import graalgo.values.GoKind;

/**
 * Cross-checks {@link DescriptorNames} (the P3-M3 replica of
 * {@code NodeFactory}'s private descriptor naming) against the real
 * factory's {@code typeDescriptorFor} / {@code kindOf} across a battery
 * of type-table shapes.
 *
 * <p>This test lives in {@code graalgo.frontend} (a phase-3 test source
 * root) solely to reach the factory's package-private naming
 * implementation; no Phase-1 source is modified. If the replica ever
 * drifts from the factory, dispatch table lookups would silently miss —
 * this test is the pin.
 *
 * <p>verifies: GG-0014
 */
public final class DescriptorNamesTest {

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static final String TYPES = """
            [{"kind": "basic", "detail": {"name": "int", "class": "int"}},
             {"kind": "named", "detail": {"name": "main.T", "underlying": 0, "methods": []}},
             {"kind": "pointer", "detail": {"elem": 1}},
             {"kind": "slice", "detail": {"elem": 0}},
             {"kind": "interface", "detail": {"empty": true, "methods": [], "embedded": []}},
             {"kind": "interface", "detail": {"empty": false,
               "methods": [{"name": "Write", "sig": 6}], "embedded": []}},
             {"kind": "signature", "detail": {"params": [{"type": 3}], "results": []}},
             {"kind": "alias", "detail": {"rhs": 1}},
             {"kind": "struct", "detail": {"fields": [{"name": "x", "type": 0}]}},
             {"kind": "map", "detail": {"key": 0, "elem": 3}},
             {"kind": "array", "detail": {"len": 4, "elem": 0}},
             {"kind": "pointer", "detail": {"elem": 2}},
             {"kind": "pointer", "detail": {"elem": 3}},
             {"kind": "named", "detail": {"name": "main.U", "underlying": 8, "methods": []}}]""";

    /**
     * Every entry's replica name equals the factory's descriptor name;
     * every entry's replica kindOf equals the factory's kindOf.
     * verifies: GG-0014
     */
    public void testNamesMatchFactory() {
        TypeTable types = new TypeTable((Json.Array) Json.parse(TYPES));
        Source source = Source.newBuilder("go", "", "test").build();
        NodeFactory factory = new NodeFactory(source);
        Json.Array objects = (Json.Array) Json.parse("[]");
        NodeFactory.BuildCtx ctx =
                new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(), types, objects);
        for (int id = 0; id < types.size(); id++) {
            String path = "test[" + id + "]";
            String actual = NodeFactory.typeDescriptorFor(ctx, id, path).name();
            String replica = DescriptorNames.descriptorName(types, id);
            assertEquals(actual, replica, "descriptor name for type " + id);
            GoKind kindActual = NodeFactory.kindOf(ctx, id, path);
            GoKind kindReplica = DescriptorNames.kindOf(types, id, path);
            assertEquals(kindActual, kindReplica, "kindOf for type " + id);
        }
    }

    /**
     * Pointer-to-named uses "*"+qualifiedName (dbd728c), NOT a structural
     * hash: two distinct named types with identical structure must not
     * share a descriptor. Pointer-to-unnamed uses Go-syntax names
     * (fa099f2), falling back to structural hashes only when no syntax
     * name exists.
     * verifies: GG-0014
     */
    public void testPointerToNamedUsesQualifiedName() {
        TypeTable types = new TypeTable((Json.Array) Json.parse(TYPES));
        // id 2 is *main.T (pointer to named).
        String p1 = DescriptorNames.descriptorName(types, 2);
        assertEquals("*main.T", p1, "pointer-to-named name");
        // id 11 is pointer-to-pointer-to-named: Go-syntax "**main.T".
        String p2 = DescriptorNames.descriptorName(types, 11);
        assertEquals("**main.T", p2, "nested pointer name");
        // id 12 is pointer-to-slice-of-int: Go-syntax "*[]int".
        String p3 = DescriptorNames.descriptorName(types, 12);
        assertEquals("*[]int", p3, "pointer-to-slice name");
    }

    /**
     * Aliases resolve to their rhs for naming (an alias IS its rhs as a
     * dynamic type).
     * verifies: GG-0014
     */
    public void testAliasResolves() {
        TypeTable types = new TypeTable((Json.Array) Json.parse(TYPES));
        assertEquals(DescriptorNames.descriptorName(types, 1),
                DescriptorNames.descriptorName(types, 7), "alias names its rhs");
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }
}
