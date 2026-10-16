package graalgo.frontend.v2;

import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.ExprStmtNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Unit tests for the v2 factory's integer {@code basiclit} handling.
 *
 * <p>Regression test for the red-master break of 2026-10-16: commit
 * 2549055e ("frontend-v2: Go→Java binary interface (not JSON)") emitted
 * {@code ConstNode.ofInt(GoKind.INT, bits)} but {@link GoKind} has no
 * {@code INT} member — the truffle module did not compile at all.
 *
 * <p>The fix resolves the literal's {@code GoKind} from the document's
 * type table through {@link GoKind#fromBasic} — the same general
 * mechanism v1's {@code NodeFactory.buildBasicLit + kindOf} uses (the
 * emitter records the go/types-computed type of every literal; see
 * {@code internal/frontend/v2/emit/nodes.go: emitBasicLit}). These tests
 * are falsifiable beyond the compile break: they assert that the kind
 * follows the type table ({@code untyped int} → {@code UNTYPED_INT},
 * {@code int64} → {@code INT64}, a named type → its underlying kind),
 * so a fix that hard-codes any single kind fails them.
 *
 * <p>Documents are encoded with a minimal writer for the v2 binary
 * format ({@link BinaryDecoder} spec); no JSON anywhere.
 */
public final class NodeFactoryV2Test {

    // --- Binary encoding (mirrors BinaryDecoder's format spec) ---

    private static final class Enc {
        private final ByteArrayOutputStream out = new ByteArrayOutputStream();

        void val(Object v) {
            if (v == null) {
                out.write(0x00);
            } else if (v instanceof Boolean b) {
                out.write(0x01);
                out.write(b ? 1 : 0);
            } else if (v instanceof Long l) {
                out.write(0x02);
                varint((l << 1) ^ (l >> 63)); // zigzag
            } else if (v instanceof Integer i) {
                val((long) i);
            } else if (v instanceof String s) {
                out.write(0x03);
                byte[] b = s.getBytes(StandardCharsets.UTF_8);
                varint(b.length);
                out.write(b, 0, b.length);
            } else if (v instanceof List<?> l) {
                out.write(0x04);
                varint(l.size());
                for (Object e : l) {
                    val(e);
                }
            } else if (v instanceof Map<?, ?> m) {
                out.write(0x05);
                varint(m.size());
                for (Map.Entry<?, ?> e : m.entrySet()) {
                    byte[] k = ((String) e.getKey()).getBytes(StandardCharsets.UTF_8);
                    varint(k.length);
                    out.write(k, 0, k.length);
                    val(e.getValue());
                }
            } else {
                throw new AssertionError("unencodable: " + v);
            }
        }

        void varint(long n) {
            while ((n & ~0x7FL) != 0) {
                out.write((int) ((n & 0x7F) | 0x80));
                n >>>= 7;
            }
            out.write((int) n);
        }

        byte[] bytes() {
            return out.toByteArray();
        }
    }

    private static Map<String, Object> map(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    private static List<Object> list(Object... items) {
        List<Object> l = new ArrayList<>();
        for (Object o : items) {
            l.add(o);
        }
        return l;
    }

    private static Map<String, Object> basicType(String name, String cls, boolean untyped) {
        return map("kind", "basic",
                "detail", map("name", name, "class", cls, "untyped", untyped));
    }

    /** Builds {@code func f() { <value> }} where the literal carries type id {@code typeId}. */
    private static byte[] docWithBasiclit(List<Object> types, int typeId, String value, String constant) {
        Map<String, Object> basiclit = map("kind", "basiclit",
                "fields", map("value", value, "type", (long) typeId, "const", constant));
        Map<String, Object> exprstmt = map("kind", "exprstmt", "fields", map("x", basiclit));
        Map<String, Object> block = map("kind", "block", "fields", map("stmts", list(exprstmt)));
        Map<String, Object> funcdecl = map("kind", "funcdecl",
                "fields", map("name", "f", "body", block));
        Map<String, Object> doc = map("frontend", "v2", "funcs", list(funcdecl), "types", types);
        Enc enc = new Enc();
        enc.val(doc);
        return enc.bytes();
    }

    private static GoExprNode buildOnlyExpr(byte[] doc) throws Exception {
        Source source = Source.newBuilder("go", "package main", "test.go").build();
        NodeFactoryV2.V2Program prog = new NodeFactoryV2(source).buildProgram(doc);
        if (prog.functionCount() != 1 || !"f".equals(prog.functions().get(0).name())) {
            throw new AssertionError("expected one function f, got " + prog.functionCount());
        }
        Node body = (Node) prog.functions().get(0).body();
        if (!(body instanceof graalgo.frontend.BlockNode)) {
            throw new AssertionError("expected BlockNode body, got " + body.getClass().getSimpleName());
        }
        Iterator<Node> stmts = body.getChildren().iterator();
        if (!stmts.hasNext() || !(stmts.next() instanceof ExprStmtNode)) {
            throw new AssertionError("expected one ExprStmtNode");
        }
        Node exprstmt = ((Node) prog.functions().get(0).body()).getChildren().iterator().next();
        Iterator<Node> exprs = exprstmt.getChildren().iterator();
        if (!exprs.hasNext() || !(exprs.next() instanceof GoExprNode)) {
            throw new AssertionError("expected one GoExprNode");
        }
        return (GoExprNode) ((Node) prog.functions().get(0).body())
                .getChildren().iterator().next()
                .getChildren().iterator().next();
    }

    private static void assertKind(byte[] doc, GoKind want, long wantBits) throws Exception {
        GoExprNode expr = buildOnlyExpr(doc);
        if (expr.kind() != want) {
            throw new AssertionError("expected kind " + want + ", got " + expr.kind());
        }
        long bits = expr.executeLong(null);
        if (bits != wantBits) {
            throw new AssertionError("expected bits " + wantBits + ", got " + bits);
        }
    }

    // --- The tests ---

    /** An untyped integer literal keeps its transparent untyped kind (v1 parity). */
    public void testUntypedIntBasiclitBuildsUntypedKind() throws Exception {
        byte[] doc = docWithBasiclit(
                list(basicType("untyped int", "int", true)), 0, "42", "42");
        assertKind(doc, GoKind.UNTYPED_INT, 42L);
    }

    /** A typed integer literal gets its declared kind from the type table. */
    public void testTypedInt64BasiclitBuildsInt64Kind() throws Exception {
        byte[] doc = docWithBasiclit(
                list(basicType("int64", "int", false)), 0, "42", "42");
        assertKind(doc, GoKind.INT64, 42L);
    }

    /** A named integer type resolves to its underlying kind (no name-keyed special case). */
    public void testNamedIntBasiclitResolvesUnderlying() throws Exception {
        List<Object> types = list(
                map("kind", "named",
                        "detail", map("name", "myint", "underlying", 1L)),
                basicType("int32", "int", false));
        byte[] doc = docWithBasiclit(types, 0, "7", "7");
        assertKind(doc, GoKind.INT32, 7L);
    }

    /** A dangling type id fails loudly, never silently. */
    public void testBadTypeIdFailsFast() throws Exception {
        byte[] doc = docWithBasiclit(
                list(basicType("untyped int", "int", true)), 7, "42", "42");
        try {
            buildOnlyExpr(doc);
        } catch (FrontendException e) {
            if (!e.getMessage().contains("out of range")) {
                throw new AssertionError("expected 'out of range', got <" + e.getMessage() + ">");
            }
            return; // expected
        }
        throw new AssertionError("dangling type id must fail with FrontendException");
    }
}
