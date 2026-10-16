package graalgo.frontend.v2;

import graalgo.frontend.FrontendException;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Unit tests for struct-field resolution in {@link NodeFactoryV2}.
 *
 * <p>Struct-typed values in the v2 wire format almost always arrive as
 * <em>named</em> types (or pointers thereto): the type table records them
 * with kind {@code "named"} whose detail carries the real struct under
 * {@code "underlying"}. These tests pin that {@link NodeFactoryV2} resolves
 * {@code named -> underlying} and {@code pointer -> elem} repeatedly before
 * demanding kind {@code "struct"} — for selector reads, selector-assignment
 * targets (both use {@code findFieldIndex}), and keyed composite literals
 * ({@code findFieldIndexByTypeId}).
 *
 * <p>Wire shapes mirror the Go emitter (internal/frontend/export.go):
 * named entries carry {@code detail.underlying}, pointer entries carry
 * {@code detail.elem}, struct entries carry {@code detail.fields}.
 */
public final class NodeFactoryV2StructFieldTest {

    // Type-table under test:
    //   0: basic int
    //   1: struct {X int; Y int}
    //   2: named Point   -> underlying 1
    //   3: *Point        -> elem 2
    //   4: **Point       -> elem 3
    //   5: named Int     -> underlying 0 (non-struct named: must still fail)
    //   6: *int          -> elem 0       (non-struct pointer: must still fail)
    private static List<Object> typeTable() {
        List<Object> types = new ArrayList<>();
        types.add(entry("basic", kv("name", "int", "class", "int", "untyped", false)));
        List<Object> fields = new ArrayList<>();
        fields.add(field("X"));
        fields.add(field("Y"));
        types.add(entry("struct", kv("fields", fields)));
        types.add(entry("named", kv("name", "Point", "underlying", 1L,
            "methods", new ArrayList<>(), "pkgpath", "")));
        types.add(entry("pointer", kv("elem", 2L, "methods", new ArrayList<>())));
        types.add(entry("pointer", kv("elem", 3L, "methods", new ArrayList<>())));
        types.add(entry("named", kv("name", "Int", "underlying", 0L,
            "methods", new ArrayList<>(), "pkgpath", "")));
        types.add(entry("pointer", kv("elem", 0L, "methods", new ArrayList<>())));
        return types;
    }

    private static Map<String, Object> entry(String kind, Map<String, Object> detail) {
        Map<String, Object> m = new HashMap<>();
        m.put("kind", kind);
        m.put("detail", detail);
        return m;
    }

    private static Map<String, Object> field(String name) {
        Map<String, Object> m = new HashMap<>();
        m.put("name", name);
        m.put("type", 0L); // int
        m.put("tag", "");
        m.put("embedded", false);
        m.put("offset", -1L);
        m.put("exported", true);
        m.put("pkgpath", "");
        return m;
    }

    private static Map<String, Object> kv(Object... kv) {
        Map<String, Object> m = new HashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    /** Selector operand map carrying the given type-table id, as the emitter produces. */
    private static Map<String, Object> xOf(long typeId) {
        Map<String, Object> m = new HashMap<>();
        m.put("kind", "ident");
        m.put("fields", kv("name", "p", "type", typeId));
        return m;
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static FrontendException expectFrontendException(Runnable r) {
        try {
            r.run();
        } catch (FrontendException e) {
            return e;
        }
        throw new AssertionError("expected FrontendException, none thrown");
    }

    // p.X where p has named struct type Point.
    public void testSelectorReadOnNamedStruct() {
        List<Object> types = typeTable();
        check(NodeFactoryV2.findFieldIndex(types, xOf(2L), "X", "test") == 0,
            "Point.X must be field 0");
        check(NodeFactoryV2.findFieldIndex(types, xOf(2L), "Y", "test") == 1,
            "Point.Y must be field 1");
    }

    // p.Y where p has type **Point: two pointer derefs plus the named step.
    public void testSelectorReadOnMultiLevelPointerToNamedStruct() {
        List<Object> types = typeTable();
        check(NodeFactoryV2.findFieldIndex(types, xOf(4L), "X", "test") == 0,
            "**Point.X must be field 0");
        check(NodeFactoryV2.findFieldIndex(types, xOf(4L), "Y", "test") == 1,
            "**Point.Y must be field 1");
    }

    // p.Y = v (selector-assignment LHS) where p has type *Point. buildAssign
    // calls the same findFieldIndex as selector reads; this pins the target
    // lookup resolves through the pointer and the named type alike.
    public void testSelectorAssignTargetThroughPointerToNamedStruct() {
        List<Object> types = typeTable();
        check(NodeFactoryV2.findFieldIndex(types, xOf(3L), "Y", "test") == 1,
            "*Point.Y (assign target) must be field 1");
    }

    // Point{X: 1}: the composite literal carries the named type id.
    public void testKeyedLiteralOnNamedStruct() {
        List<Object> types = typeTable();
        check(NodeFactoryV2.findFieldIndexByTypeId(types, 2, "X", "test") == 0,
            "Point{X} must be field 0");
        check(NodeFactoryV2.findFieldIndexByTypeId(types, 2, "Y", "test") == 1,
            "Point{Y} must be field 1");
    }

    // &Point{Y: 2}: the composite literal carries the pointer type id.
    public void testKeyedLiteralOnPointerToNamedStruct() {
        List<Object> types = typeTable();
        check(NodeFactoryV2.findFieldIndexByTypeId(types, 3, "Y", "test") == 1,
            "&Point{Y} must be field 1");
    }

    // Negative control: a named type whose underlying type is not a struct
    // must still fail loudly — the resolver must not invent struct-ness.
    public void testSelectorOnNamedNonStructFails() {
        List<Object> types = typeTable();
        FrontendException e = expectFrontendException(
            () -> NodeFactoryV2.findFieldIndex(types, xOf(5L), "X", "test"));
        check(e.getMessage().contains("non-struct"),
            "must name non-struct, got: " + e.getMessage());
    }

    // Negative control: pointer to non-struct still fails after resolution.
    public void testSelectorOnPointerToNonStructFails() {
        List<Object> types = typeTable();
        FrontendException e = expectFrontendException(
            () -> NodeFactoryV2.findFieldIndex(types, xOf(6L), "X", "test"));
        check(e.getMessage().contains("non-struct"),
            "must name non-struct, got: " + e.getMessage());
    }

    // Negative control: unknown field names fail loudly, not with a bogus index.
    public void testMissingFieldFails() {
        List<Object> types = typeTable();
        FrontendException e = expectFrontendException(
            () -> NodeFactoryV2.findFieldIndexByTypeId(types, 2, "Z", "test"));
        check(e.getMessage().contains("not found"),
            "must name missing field, got: " + e.getMessage());
    }

    // Negative control: a cyclic pointer chain must fail, never loop forever.
    public void testCyclicTypeChainFails() {
        List<Object> types = new ArrayList<>();
        types.add(entry("pointer", kv("elem", 1L)));
        types.add(entry("pointer", kv("elem", 0L)));
        FrontendException e = expectFrontendException(
            () -> NodeFactoryV2.resolveTransparentTypeId(types, 0, "test"));
        check(e.getMessage().contains("cyclic"),
            "must name the cycle, got: " + e.getMessage());
    }

    // Negative control: out-of-range ids fail loudly at the resolver.
    public void testBadTypeIdFails() {
        List<Object> types = typeTable();
        FrontendException e = expectFrontendException(
            () -> NodeFactoryV2.resolveTransparentTypeId(types, 99, "test"));
        check(e.getMessage().contains("bad type id"),
            "must name bad type id, got: " + e.getMessage());
    }
}
