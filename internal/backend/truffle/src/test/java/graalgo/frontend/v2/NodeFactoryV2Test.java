package graalgo.frontend.v2;

import com.oracle.truffle.api.source.Source;
import graalgo.frontend.FrontendException;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Fail-loud tests for multi-value constructs in the v2 node factory.
 *
 * <p>The v2 Truffle pipeline (calls, {@code ReturnNode}, assignments) is
 * single-valued end to end: there is no tuple/multi-value representation.
 * These tests pin the contract that every multi-value construct either
 * works or fails <em>loudly</em> with {@link FrontendException} — never
 * silently producing wrong code (e.g. returning only the first of several
 * results, or duplicating one assigned value across several targets).
 *
 * <p>Each loud-failure test is written against the pre-fix behavior and
 * fails on it: before the fix the factory silently built the wrong tree,
 * so no exception was thrown and the test's expected {@code FrontendException}
 * never arrived.
 */
public final class NodeFactoryV2Test {

    private final NodeFactoryV2 factory =
        new NodeFactoryV2(Source.newBuilder("v2test", "", "NodeFactoryV2Test").build());

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    // --- Wire-format builders (hand-built v2 document fragments) ---

    private static Map<String, Object> fields(Object... kv) {
        Map<String, Object> m = new HashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    private static Map<String, Object> node(String kind, Map<String, Object> f) {
        Map<String, Object> m = new HashMap<>();
        m.put("kind", kind);
        m.put("fields", f);
        return m;
    }

    /** types[0] = basic int. */
    private static List<Object> intTypes() {
        List<Object> types = new ArrayList<>();
        types.add(fields("kind", "basic",
            "detail", fields("name", "int", "class", "int")));
        return types;
    }

    /** types[0] = basic int, types[1] = func signature. */
    private static List<Object> intAndFuncTypes() {
        List<Object> types = intTypes();
        types.add(fields("kind", "signature"));
        return types;
    }

    /** types[0] = basic int, types[1] = map. */
    private static List<Object> intAndMapTypes() {
        List<Object> types = intTypes();
        types.add(fields("kind", "map"));
        return types;
    }

    /** objects[i] = {type: typeId}. */
    private static List<Object> objects(long... typeIds) {
        List<Object> objs = new ArrayList<>();
        for (long t : typeIds) {
            objs.add(fields("type", t));
        }
        return objs;
    }

    private static Map<String, Object> ident(long objId, String name) {
        return node("ident", fields("obj", objId, "name", name));
    }

    /** Ident with an explicit expression type id (as the Go emitter provides). */
    private static Map<String, Object> ident(long objId, String name, long typeId) {
        return node("ident", fields("obj", objId, "name", name, "type", typeId));
    }

    private static Map<String, Object> lit(String value) {
        return node("basiclit", fields("value", value, "type", 0L));
    }

    private static Map<String, Object> ret(Map<String, Object>... results) {
        return node("return", fields("results", List.of((Object[]) results)));
    }

    private static Map<String, Object> block(Map<String, Object>... stmts) {
        return node("block", fields("stmts", List.of((Object[]) stmts)));
    }

    private static Map<String, Object> assign(String tok,
            List<Map<String, Object>> lhs, List<Map<String, Object>> rhs) {
        return node("assign", fields("tok", tok,
            "lhs", new ArrayList<Object>(lhs),
            "rhs", new ArrayList<Object>(rhs)));
    }

    private static Map<String, Object> vardecl(List<String> names, List<Long> objs,
            List<Map<String, Object>> values) {
        return node("vardecl", fields("names", new ArrayList<Object>(names),
            "objs", new ArrayList<Object>(objs),
            "values", new ArrayList<Object>(values)));
    }

    private static Map<String, Object> call(Map<String, Object> fun) {
        return node("call", fields("fun", fun, "args", new ArrayList<Object>(), "type", 0L));
    }

    private static Map<String, Object> funcdecl(Map<String, Object> body) {
        return node("funcdecl", fields("name", "testfn",
            "params", new ArrayList<Object>(), "body", body));
    }

    private NodeFactoryV2.V2Function build(Map<String, Object> fn,
            List<Object> types, List<Object> objects) {
        return factory.buildFunction(fn, 0, types, objects);
    }

    private static FrontendException expectFrontendException(Runnable r, String what) {
        try {
            r.run();
        } catch (FrontendException e) {
            return e;
        }
        throw new AssertionError(what + " built silently (no FrontendException)");
    }

    // --- V2-2: multi-value return ---

    public void testMultiValueReturnFailsLoudly() {
        // return 1, 2 — valid Go. The pre-fix factory silently returned 1.
        Map<String, Object> fn = funcdecl(block(ret(lit("1"), lit("2"))));
        FrontendException e = expectFrontendException(
            () -> build(fn, intTypes(), List.of()), "multi-value return");
        check(e.getMessage().contains("multi-value return"),
            "message names the construct, got: " + e.getMessage());
    }

    public void testSingleValueReturnStillBuilds() {
        // Regression guard: the loud failure must not break the common case.
        Map<String, Object> fn = funcdecl(block(ret(lit("1"))));
        NodeFactoryV2.V2Function f = build(fn, intTypes(), List.of());
        check(f != null && f.body() != null, "single-value return must build");
    }

    // --- V2-3: multi-target assignment from a single RHS ---

    public void testMultiValueAssignFromCallFailsLoudly() {
        // var f func(); var a, b int; a, b = f() — valid Go. The pre-fix
        // factory silently assigned f()'s single value to both a and b.
        Map<String, Object> fn = funcdecl(block(
            vardecl(List.of("f"), List.of(2L), List.of()),
            vardecl(List.of("a", "b"), List.of(0L, 1L), List.of()),
            assign("=", List.of(ident(0L, "a"), ident(1L, "b")),
                List.of(call(ident(2L, "f"))))));
        FrontendException e = expectFrontendException(
            () -> build(fn, intAndFuncTypes(), objects(0L, 0L, 1L)),
            "multi-value assignment from call");
        check(e.getMessage().contains("multi-value assignment"),
            "message names the construct, got: " + e.getMessage());
    }

    public void testMultiValueAssignFromMapIndexFailsLoudly() {
        // var m map[int]int; var v, ok int; v, ok := m[0] — the two-value
        // map-index form. The pre-fix factory silently assigned the looked-up
        // value to both v and ok.
        Map<String, Object> idx = node("index", fields(
            "x", ident(0L, "m", 1L), "index", lit("0"), "type", 0L));
        Map<String, Object> fn = funcdecl(block(
            vardecl(List.of("m"), List.of(0L), List.of()),
            assign(":=", List.of(ident(1L, "v"), ident(2L, "ok")), List.of(idx))));
        FrontendException e = expectFrontendException(
            () -> build(fn, intAndMapTypes(), objects(1L, 0L, 0L)),
            "multi-value assignment from map index");
        check(e.getMessage().contains("multi-value assignment"),
            "message names the construct, got: " + e.getMessage());
    }

    public void testMismatchedAssignFailsLoudly() {
        // a, b := 1 — invalid Go (not a multi-valued RHS). The pre-fix
        // factory silently assigned 1 to both.
        Map<String, Object> fn = funcdecl(block(
            assign(":=", List.of(ident(0L, "a"), ident(1L, "b")), List.of(lit("1")))));
        FrontendException e = expectFrontendException(
            () -> build(fn, intTypes(), objects(0L, 0L)),
            "mismatched assignment");
        check(e.getMessage().contains("mismatched"),
            "message names the mismatch, got: " + e.getMessage());
    }

    public void testPairedAssignStillBuilds() {
        // Regression guard: a, b := 1, 2 must keep working.
        Map<String, Object> fn = funcdecl(block(
            assign(":=", List.of(ident(0L, "a"), ident(1L, "b")),
                List.of(lit("1"), lit("2")))));
        NodeFactoryV2.V2Function f = build(fn, intTypes(), objects(0L, 0L));
        check(f != null && f.body() != null, "paired assignment must build");
    }

    // --- Same bug class in var declarations: var a, b = f() ---

    public void testMultiValueVarInitFailsLoudly() {
        // var a, b = f() — valid Go. The pre-fix factory silently
        // initialized only a, leaving b at its zero value.
        Map<String, Object> fn = funcdecl(block(
            vardecl(List.of("f"), List.of(2L), List.of()),
            vardecl(List.of("a", "b"), List.of(0L, 1L),
                List.of(call(ident(2L, "f"))))));
        FrontendException e = expectFrontendException(
            () -> build(fn, intAndFuncTypes(), objects(0L, 0L, 1L)),
            "multi-value var initialization");
        check(e.getMessage().contains("multi-value var initialization"),
            "message names the construct, got: " + e.getMessage());
    }

    public void testZeroValueVarDeclStillBuilds() {
        // Regression guard: var a, b int (no values) must keep working.
        Map<String, Object> fn = funcdecl(block(
            vardecl(List.of("a", "b"), List.of(0L, 1L), List.of())));
        NodeFactoryV2.V2Function f = build(fn, intTypes(), objects(0L, 0L));
        check(f != null && f.body() != null, "zero-value var decl must build");
    }
}
