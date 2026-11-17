package graalgo.frontend.v2;

import java.util.List;
import java.util.Map;

import graalgo.frontend.FrontendException;
import graalgo.values.GoKind;

/**
 * Unit tests for the v2 type-table → GoKind mapping
 * ({@code NodeFactoryV2#goKindForTypeId}).
 *
 * <p>The emitter now describes generics, aliases, unions, type params,
 * and call-result tuples (independent review 2026-11-17, P0-1); this
 * test pins how the factory resolves each new kind — and that an
 * unknown kind fails loudly instead of silently resolving to
 * INTERFACE, as the old default arm did.
 */
public final class NodeFactoryV2TypeKindTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Map<String, Object> basic(String name, String cls) {
        return Map.of("kind", "basic",
                "detail", Map.of("name", name, "class", cls));
    }

    private static Map<String, Object> structKind() {
        return Map.of("kind", "struct", "detail", Map.of("fields", List.of()));
    }

    private static Map<String, Object> interfaceKind() {
        return Map.of("kind", "interface",
                "detail", Map.of("empty", Boolean.TRUE, "methods", List.of(), "embedded", List.of()));
    }

    private static Map<String, Object> named(long underlying, List<Object> typeargs) {
        return Map.of("kind", "named",
                "detail", Map.of("name", "main.List", "underlying", Long.valueOf(underlying),
                        "methods", List.of(), "typeparams", List.of(), "typeargs", typeargs));
    }

    private static Map<String, Object> typeparam(long constraint) {
        return Map.of("kind", "typeparam",
                "detail", Map.of("name", "T", "constraint", Long.valueOf(constraint),
                        "index", Long.valueOf(0)));
    }

    private static Map<String, Object> alias(long rhs) {
        return Map.of("kind", "alias",
                "detail", Map.of("name", "main.MyInt", "rhs", Long.valueOf(rhs)));
    }

    private static Map<String, Object> union(List<Object> terms) {
        return Map.of("kind", "union", "detail", Map.of("terms", terms));
    }

    private static Map<String, Object> tuple(List<Object> elems) {
        return Map.of("kind", "tuple", "detail", Map.of("elems", elems));
    }

    private static GoKind kindOf(List<Object> types, int id) {
        return NodeFactoryV2.goKindForTypeId(types, Long.valueOf(id), "test");
    }

    private static void expectFrontendException(Runnable r, String what) {
        try {
            r.run();
        } catch (FrontendException e) {
            return;
        }
        throw new AssertionError("expected FrontendException for " + what);
    }

    public void testBasicKinds() {
        List<Object> types = List.of(basic("int", "int"), basic("string", "string"), basic("bool", "bool"));
        check(kindOf(types, 0) == GoKind.INT64, "int -> INT64");
        check(kindOf(types, 1) == GoKind.STRING, "string -> STRING");
        check(kindOf(types, 2) == GoKind.BOOL, "bool -> BOOL");
    }

    public void testBasicUnknownClassThrows() {
        List<Object> types = List.of(basic("()", "unknown"));
        expectFrontendException(() -> kindOf(types, 0), "basic class unknown");
    }

    public void testNamedResolvesUnderlyingWithTypeArgs() {
        // List[int]: named over a struct, type arg int.
        List<Object> types = List.of(basic("int", "int"), structKind(),
                named(1, List.of(Long.valueOf(0))));
        check(kindOf(types, 2) == GoKind.STRUCT, "List[int] -> STRUCT via underlying");
        // Named over a basic type keeps the basic kind.
        List<Object> types2 = List.of(basic("int", "int"),
                named(0, List.of(Long.valueOf(0))));
        check(kindOf(types2, 1) == GoKind.INT64, "named over int -> INT64");
    }

    public void testNamedWithoutUnderlyingFallsBack() {
        Map<String, Object> noUnderlying = Map.of("kind", "named",
                "detail", Map.of("name", "main.X", "methods", List.of(),
                        "typeparams", List.of(), "typeargs", List.of()));
        List<Object> types = List.of(noUnderlying);
        check(kindOf(types, 0) == GoKind.INTERFACE, "named without underlying -> INTERFACE");
    }

    public void testTypeparamResolvesConstraint() {
        List<Object> types = List.of(basic("int", "int"), interfaceKind(),
                typeparam(0), typeparam(1));
        check(kindOf(types, 2) == GoKind.INT64, "typeparam constrained by int -> INT64");
        check(kindOf(types, 3) == GoKind.INTERFACE, "typeparam constrained by any -> INTERFACE");
    }

    public void testTypeparamWithoutConstraintThrows() {
        Map<String, Object> noConstraint = Map.of("kind", "typeparam",
                "detail", Map.of("name", "T", "index", Long.valueOf(0)));
        List<Object> types = List.of(noConstraint);
        expectFrontendException(() -> kindOf(types, 0), "typeparam without constraint");
    }

    public void testAliasResolvesRhs() {
        List<Object> types = List.of(basic("string", "string"), interfaceKind(),
                alias(0), alias(1));
        check(kindOf(types, 2) == GoKind.STRING, "alias of string -> STRING");
        check(kindOf(types, 3) == GoKind.INTERFACE, "alias of any -> INTERFACE");
    }

    public void testAliasWithoutRhsThrows() {
        Map<String, Object> noRhs = Map.of("kind", "alias", "detail", Map.of("name", "main.X"));
        List<Object> types = List.of(noRhs);
        expectFrontendException(() -> kindOf(types, 0), "alias without rhs");
    }

    public void testUnionThrows() {
        Map<String, Object> term = Map.of("type", Long.valueOf(0), "tilde", Boolean.TRUE);
        List<Object> types = List.of(basic("int", "int"), union(List.of(term)));
        expectFrontendException(() -> kindOf(types, 1), "union has no value kind");
    }

    public void testTupleKinds() {
        List<Object> types = List.of(basic("int", "int"), basic("string", "string"),
                tuple(List.of()), tuple(List.of(Long.valueOf(0), Long.valueOf(1))));
        check(kindOf(types, 2) == GoKind.INTERFACE, "empty tuple (void call) -> INTERFACE");
        expectFrontendException(() -> kindOf(types, 3), "multi-result tuple has no single kind");
    }

    public void testUnknownKindThrows() {
        Map<String, Object> unknown = Map.of("kind", "frobnicate", "detail", Map.of());
        List<Object> types = List.of(unknown);
        expectFrontendException(() -> kindOf(types, 0), "unknown kind (was silent INTERFACE)");
    }
}
