package graalgo.frontend.v2;

import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.FrontendException;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.lang.reflect.Field;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Fail-fast tests for multi-value shapes in frontend/v2 (lane H1).
 *
 * <p>v2 has no tuple or unpacking representation yet, so the factory must
 * reject multi-value constructs loudly instead of silently dropping
 * values: a multi-result return used to return only the first result,
 * a single-RHS/multi-LHS assignment reused one child node for every
 * LHS, and a var declaration with a count mismatch truncated the extra
 * names/values. Each negative test below feeds the factory a document
 * in the exact shape the Go emitter produces for the corresponding Go
 * program (see emit/multivalue_test.go on the Go side) and asserts a
 * FrontendException naming multi-value support. The positive controls
 * prove the rejection is not over-broad: matched-count shapes and
 * single-valued calls still build.
 *
 * <p>Documents are encoded with a small test-only encoder mirroring
 * internal/frontend/v2/encode/encode.go (magic "g2bi", version 1,
 * tag + zigzag-varint ints, length-prefixed strings, lists, maps).
 * No Go process is involved at Java test time.
 */
public final class NodeFactoryV2MultiValueTest {

    // Type-table ids shared by the test documents.
    private static final long INT = 0L;
    private static final long SIG_SINGLE = 1L; // () -> int
    private static final long SIG_MULTI = 2L; // () -> (int, int)
    private static final long TUPLE2 = 3L; // (int, int)

    // Object-table ids shared by the test documents.
    private static final long OBJ_A = 0L;
    private static final long OBJ_B = 1L;
    private static final long OBJ_F1 = 2L;
    private static final long OBJ_F2 = 3L;

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Source source() {
        return Source.newBuilder("go", "package main", "test.go").build();
    }

    // --- Test-only binary encoder (mirror of encode/encode.go) ---

    private static void writeVarint(ByteArrayOutputStream out, long value) {
        long u = value;
        while (true) {
            int b = (int) (u & 0x7F);
            u >>>= 7;
            if (u == 0) {
                out.write(b);
                return;
            }
            out.write(b | 0x80);
        }
    }

    private static void encodeValue(ByteArrayOutputStream out, Object v) {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof Boolean b) {
            out.write(0x01);
            out.write(b ? 1 : 0);
        } else if (v instanceof Long l) {
            out.write(0x02);
            writeVarint(out, (l << 1) ^ (l >> 63));
        } else if (v instanceof Integer i) {
            out.write(0x02);
            long l = i;
            writeVarint(out, (l << 1) ^ (l >> 63));
        } else if (v instanceof String s) {
            byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
            out.write(0x03);
            writeVarint(out, bytes.length);
            out.writeBytes(bytes);
        } else if (v instanceof List<?> l) {
            out.write(0x04);
            writeVarint(out, l.size());
            for (Object e : l) {
                encodeValue(out, e);
            }
        } else if (v instanceof Map<?, ?> m) {
            out.write(0x05);
            writeVarint(out, m.size());
            List<String> keys = new ArrayList<>();
            for (Object k : m.keySet()) {
                keys.add((String) k);
            }
            Collections.sort(keys);
            for (String k : keys) {
                byte[] bytes = k.getBytes(StandardCharsets.UTF_8);
                writeVarint(out, bytes.length);
                out.writeBytes(bytes);
                encodeValue(out, m.get(k));
            }
        } else {
            throw new IllegalArgumentException("cannot encode " + v);
        }
    }

    private static byte[] encodeDocument(Map<String, Object> doc) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.writeBytes("g2bi".getBytes(StandardCharsets.UTF_8));
        writeVarint(out, 1);
        encodeValue(out, doc);
        return out.toByteArray();
    }

    // --- Document builders ---

    private static Map<String, Object> node(String kind, Map<String, Object> fields) {
        Map<String, Object> pos = new LinkedHashMap<>();
        pos.put("file", 0L);
        pos.put("line", 1L);
        pos.put("col", 1L);
        pos.put("off", 0L);
        pos.put("len", 1L);
        Map<String, Object> n = new LinkedHashMap<>();
        n.put("kind", kind);
        n.put("pos", pos);
        n.put("fields", fields);
        return n;
    }

    private static Map<String, Object> basiclit(String value, long type) {
        Map<String, Object> f = new LinkedHashMap<>();
        f.put("value", value);
        f.put("type", type);
        return node("basiclit", f);
    }

    private static Map<String, Object> ident(String name, Long obj, Long type) {
        Map<String, Object> f = new LinkedHashMap<>();
        f.put("name", name);
        if (obj != null) {
            f.put("obj", obj);
        }
        if (type != null) {
            f.put("type", type);
        }
        return node("ident", f);
    }

    private static Map<String, Object> param(String name, long obj, long type) {
        Map<String, Object> f = new LinkedHashMap<>();
        f.put("name", name);
        f.put("obj", obj);
        f.put("type", type);
        return node("param", f);
    }

    private static Map<String, Object> call(Map<String, Object> fun, long type) {
        Map<String, Object> f = new LinkedHashMap<>();
        f.put("fun", fun);
        f.put("args", List.of());
        f.put("builtin", null);
        f.put("type", type);
        f.put("ellipsis", false);
        return node("call", f);
    }

    private static Map<String, Object> block(List<Object> stmts) {
        return node("block", Map.of("stmts", stmts));
    }

    private static Map<String, Object> funcdecl(String name, List<Object> params, Map<String, Object> body) {
        Map<String, Object> f = new LinkedHashMap<>();
        f.put("name", name);
        f.put("sig", 0L);
        f.put("params", params);
        f.put("body", body);
        f.put("pkgpath", "main");
        return node("funcdecl", f);
    }

    private static Map<String, Object> document(List<Object> funcs, List<Object> types, List<Object> objects) {
        Map<String, Object> d = new LinkedHashMap<>();
        d.put("frontend", "v2");
        d.put("generator", Map.of("tool", "test"));
        d.put("package", Map.of("name", "main"));
        d.put("types", types);
        d.put("objects", objects);
        d.put("funcs", funcs);
        d.put("globals", List.of());
        return d;
    }

    private static Map<String, Object> intType() {
        return Map.of("kind", "basic",
                "detail", Map.of("name", "int", "class", "int", "untyped", false));
    }

    private static Map<String, Object> signatureType(List<Object> results) {
        Map<String, Object> detail = new LinkedHashMap<>();
        detail.put("params", List.of());
        detail.put("results", results);
        detail.put("variadic", false);
        return Map.of("kind", "signature", "detail", detail);
    }

    private static Map<String, Object> tupleType(List<Object> results) {
        return Map.of("kind", "tuple", "detail", Map.of("results", results));
    }

    private static Map<String, Object> resultEntry(long type) {
        return Map.of("name", "", "type", type);
    }

    private static List<Object> baseTypes() {
        return List.of(
                intType(),
                signatureType(List.of(resultEntry(INT))),
                signatureType(List.of(resultEntry(INT), resultEntry(INT))),
                tupleType(List.of(resultEntry(INT), resultEntry(INT))));
    }

    private static List<Object> baseObjects() {
        return List.of(
                Map.of("kind", "var", "name", "a", "type", INT),
                Map.of("kind", "var", "name", "b", "type", INT),
                Map.of("kind", "func", "name", "f1", "type", SIG_SINGLE),
                Map.of("kind", "func", "name", "f2", "type", SIG_MULTI));
    }

    private static NodeFactoryV2.V2Program build(Map<String, Object> doc) throws IOException {
        return new NodeFactoryV2(source()).buildProgram(encodeDocument(doc));
    }

    private static FrontendException expectBuildFailure(Map<String, Object> doc, String wantSubstring) {
        try {
            build(doc);
        } catch (FrontendException e) {
            check(e.getMessage() != null && e.getMessage().contains(wantSubstring),
                    "error " + e.getMessage() + " does not contain " + wantSubstring);
            return e;
        } catch (IOException e) {
            throw new AssertionError("unexpected IOException: " + e.getMessage(), e);
        }
        throw new AssertionError("expected FrontendException containing " + wantSubstring + ", but build succeeded");
    }

    private static void collect(Node n, String simpleName, List<Node> out) {
        if (n.getClass().getSimpleName().equals(simpleName)) {
            out.add(n);
        }
        for (Node ch : n.getChildren()) {
            collect(ch, simpleName, out);
        }
    }

    private static Object childValue(Node n) throws Exception {
        Field f = n.getClass().getDeclaredField("value");
        f.setAccessible(true);
        return f.get(n);
    }

    // --- Negative tests: multi-value shapes must fail loudly ---

    public void testMultiValueReturnRejected() throws Exception {
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(),
                        block(List.of(node("return", Map.of("results",
                                List.of(basiclit("1", INT), basiclit("2", INT)))))))),
                List.of(intType()), List.of());
        expectBuildFailure(doc, "multi-value");
    }

    public void testMultiValueReturnWithSideEffectingSecondResultRejected() throws Exception {
        // Second result is a call (side-effecting in general): the
        // factory must reject the return rather than build a tree that
        // can never evaluate it. The return check fires before any
        // result is built, so the call's own validity is irrelevant.
        Map<String, Object> ret = node("return", Map.of("results", List.of(
                basiclit("1", INT),
                call(ident("f1", OBJ_F1, SIG_SINGLE), INT))));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(param("f1", OBJ_F1, SIG_SINGLE)), block(List.of(ret)))),
                baseTypes(), baseObjects());
        expectBuildFailure(doc, "multi-value");
    }

    public void testSingleRhsMultiLhsAssignRejected() throws Exception {
        // Shape of `a, b = <single value>` at factory level: the reuse
        // bug was here (one child node for both LHS targets).
        Map<String, Object> as = node("assign", Map.of(
                "tok", ":=",
                "lhs", List.of(ident("a", OBJ_A, INT), ident("b", OBJ_B, INT)),
                "rhs", List.of(basiclit("7", INT))));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(), block(List.of(as)))),
                List.of(intType()), baseObjects());
        expectBuildFailure(doc, "multi-value");
    }

    public void testSingleRhsMultiLhsCallAssignRejected() throws Exception {
        // The checker-valid shape `a, b = f()` where f returns two
        // values (f here is a function-valued parameter, so the call
        // itself is buildable and the assignment check is what fires).
        Map<String, Object> as = node("assign", Map.of(
                "tok", ":=",
                "lhs", List.of(ident("a", OBJ_A, INT), ident("b", OBJ_B, INT)),
                "rhs", List.of(call(ident("f2", OBJ_F2, SIG_MULTI), TUPLE2))));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(param("f2", OBJ_F2, SIG_MULTI)), block(List.of(as)))),
                baseTypes(), baseObjects());
        expectBuildFailure(doc, "multi-value");
    }

    public void testVarDeclCountMismatchRejected() throws Exception {
        // Shape of `var a, b = f()` at factory level: one value, two
        // names — b used to be left at its zero value silently.
        Map<String, Object> vd = new LinkedHashMap<>();
        vd.put("names", List.of("a", "b"));
        vd.put("objs", List.of(OBJ_A, OBJ_B));
        vd.put("define", false);
        vd.put("type", null);
        vd.put("values", List.of(basiclit("7", INT)));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(), block(List.of(node("vardecl", vd))))),
                List.of(intType()), baseObjects());
        expectBuildFailure(doc, "multi-value");
    }

    public void testVarDeclExtraValuesRejected() throws Exception {
        // The other truncation direction: extra values used to have
        // their side effects silently dropped by the loop bound.
        Map<String, Object> vd = new LinkedHashMap<>();
        vd.put("names", List.of("a"));
        vd.put("objs", List.of(OBJ_A));
        vd.put("define", false);
        vd.put("type", null);
        vd.put("values", List.of(basiclit("1", INT), basiclit("2", INT)));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(), block(List.of(node("vardecl", vd))))),
                List.of(intType()), baseObjects());
        expectBuildFailure(doc, "multi-value");
    }

    public void testMultiValueCallAsSingleResultRejected() throws Exception {
        // Shape of `return f()` where f returns two values: the call's
        // type is a tuple, which has no single GoKind.
        Map<String, Object> ret = node("return", Map.of("results",
                List.of(call(ident("f2", OBJ_F2, SIG_MULTI), TUPLE2))));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(param("f2", OBJ_F2, SIG_MULTI)), block(List.of(ret)))),
                baseTypes(), baseObjects());
        expectBuildFailure(doc, "multi-value");
    }

    // --- Positive controls: matched shapes and single values still build ---

    public void testSingleValueReturnBuilds() throws Exception {
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(),
                        block(List.of(node("return", Map.of("results", List.of(basiclit("42", INT)))))))),
                List.of(intType()), List.of());
        NodeFactoryV2.V2Program p = build(doc);
        check(p.functionCount() == 1, "1 function");
        check(p.functions().get(0).hasBody(), "main has a body");
    }

    public void testMatchedMultiAssignBuilds() throws Exception {
        // `a, b := 1, 2`: paired positionally, with distinct RHS nodes.
        Map<String, Object> as = node("assign", Map.of(
                "tok", ":=",
                "lhs", List.of(ident("a", OBJ_A, INT), ident("b", OBJ_B, INT)),
                "rhs", List.of(basiclit("1", INT), basiclit("2", INT))));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(), block(List.of(as)))),
                List.of(intType()), baseObjects());
        NodeFactoryV2.V2Program p = build(doc);
        List<Node> assigns = new ArrayList<>();
        collect((Node) p.functions().get(0).body(), "AssignNode", assigns);
        check(assigns.size() == 2, "2 AssignNodes, got " + assigns.size());
        check(childValue(assigns.get(0)) != childValue(assigns.get(1)),
                "matched assign must not share one RHS node between LHS targets");
    }

    public void testMatchedVarDeclBuilds() throws Exception {
        Map<String, Object> vd = new LinkedHashMap<>();
        vd.put("names", List.of("a", "b"));
        vd.put("objs", List.of(OBJ_A, OBJ_B));
        vd.put("define", false);
        vd.put("type", null);
        vd.put("values", List.of(basiclit("1", INT), basiclit("2", INT)));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(), block(List.of(node("vardecl", vd))))),
                List.of(intType()), baseObjects());
        NodeFactoryV2.V2Program p = build(doc);
        List<Node> assigns = new ArrayList<>();
        collect((Node) p.functions().get(0).body(), "AssignNode", assigns);
        check(assigns.size() == 2, "2 AssignNodes for 2 names, got " + assigns.size());
    }

    public void testVarDeclWithoutValuesBuilds() throws Exception {
        // `var a, b` (no initializer): zero values are legitimate and
        // must not trip the count-mismatch check.
        Map<String, Object> vd = new LinkedHashMap<>();
        vd.put("names", List.of("a", "b"));
        vd.put("objs", List.of(OBJ_A, OBJ_B));
        vd.put("define", false);
        vd.put("type", null);
        vd.put("values", List.of());
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(), block(List.of(node("vardecl", vd))))),
                List.of(intType()), baseObjects());
        NodeFactoryV2.V2Program p = build(doc);
        check(p.functions().get(0).hasBody(), "main has a body");
        List<Node> assigns = new ArrayList<>();
        collect((Node) p.functions().get(0).body(), "AssignNode", assigns);
        check(assigns.isEmpty(), "no AssignNodes without values, got " + assigns.size());
    }

    public void testSingleValueCallBuilds() throws Exception {
        // A single-valued call must not trip the tuple rejection.
        Map<String, Object> ret = node("return", Map.of("results",
                List.of(call(ident("f1", OBJ_F1, SIG_SINGLE), INT))));
        Map<String, Object> doc = document(
                List.of(funcdecl("main", List.of(param("f1", OBJ_F1, SIG_SINGLE)), block(List.of(ret)))),
                baseTypes(), baseObjects());
        NodeFactoryV2.V2Program p = build(doc);
        List<Node> calls = new ArrayList<>();
        collect((Node) p.functions().get(0).body(), "FuncValueCallNode", calls);
        check(calls.size() == 1, "1 call node, got " + calls.size());
    }
}
