package graalgo.frontend.v2;

import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoStmtNode;
import graalgo.frontend.MakeNode;
import graalgo.values.GoKind;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * L18: the v2 {@code make} builtin must dispatch on the result kind —
 * MAP → map node, SLICE → slice node — and fail loudly for CHAN (Phase-2
 * scope, GG-0020) and any other kind, instead of silently building a
 * slice node.
 *
 * <p>Documents are encoded with a minimal in-test binary writer that
 * mirrors {@code internal/frontend/v2/encode} (magic "g2bi", version 1,
 * tag + zigzag-varint ints, length-prefixed strings, lists, maps).
 */
public final class NodeFactoryV2MakeTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    // --- minimal binary encoder (mirrors internal/frontend/v2/encode) ---

    private static void writeVarint(ByteArrayOutputStream out, long x) {
        long v = x;
        while ((v & ~0x7FL) != 0) {
            out.write((byte) ((v & 0x7F) | 0x80));
            v >>>= 7;
        }
        out.write((byte) v);
    }

    private static void writeZigzag(ByteArrayOutputStream out, long n) {
        writeVarint(out, (n << 1) ^ (n >> 63));
    }

    private static void writeValue(ByteArrayOutputStream out, Object v) {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof Boolean b) {
            out.write(0x01);
            out.write(b ? 1 : 0);
        } else if (v instanceof Long l) {
            out.write(0x02);
            writeZigzag(out, l);
        } else if (v instanceof Integer i) {
            out.write(0x02);
            writeZigzag(out, i.longValue());
        } else if (v instanceof String s) {
            byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
            out.write(0x03);
            writeVarint(out, bytes.length);
            out.write(bytes, 0, bytes.length);
        } else if (v instanceof List<?> list) {
            out.write(0x04);
            writeVarint(out, list.size());
            for (Object e : list) {
                writeValue(out, e);
            }
        } else if (v instanceof Map<?, ?> map) {
            out.write(0x05);
            writeVarint(out, map.size());
            for (Map.Entry<?, ?> e : map.entrySet()) {
                byte[] key = ((String) e.getKey()).getBytes(StandardCharsets.UTF_8);
                writeVarint(out, key.length);
                out.write(key, 0, key.length);
                writeValue(out, e.getValue());
            }
        } else {
            throw new AssertionError("unencodable: " + v.getClass());
        }
    }

    private static Map<String, Object> mapOf(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    private static Map<String, Object> node(String kind, Map<String, Object> fields) {
        return mapOf("kind", kind, "fields", fields);
    }

    /**
     * Builds a document whose main() body is a single expression statement
     * {@code make(...)} with result type id 1 (types[0] is always int).
     */
    private static byte[] makeDoc(String typeKind, Map<String, Object> typeDetail,
            List<Object> args) throws IOException {
        Map<String, Object> callFields = new LinkedHashMap<>();
        callFields.put("builtin", "make");
        callFields.put("fun", node("ident", mapOf("name", "make", "builtin", "make")));
        callFields.put("args", args);
        callFields.put("type", 1L);

        Map<String, Object> exprstmt = node("exprstmt", mapOf("x", node("call", callFields)));
        Map<String, Object> body = node("block", mapOf("stmts", List.of(exprstmt)));
        Map<String, Object> funcdecl = node("funcdecl",
                mapOf("name", "main", "params", new ArrayList<>(), "body", body));

        List<Object> types = List.of(
                mapOf("kind", "basic", "detail", mapOf("name", "int", "class", "int")),
                mapOf("kind", typeKind, "detail", typeDetail));

        Map<String, Object> doc = new LinkedHashMap<>();
        doc.put("frontend", "v2");
        doc.put("package", mapOf("name", "main", "files", new ArrayList<>(), "lang", "go1.27"));
        doc.put("types", types);
        doc.put("objects", new ArrayList<>());
        doc.put("funcs", List.of(funcdecl));
        doc.put("globals", new ArrayList<>());

        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write("g2bi".getBytes(StandardCharsets.UTF_8));
        out.write(0x01);
        writeValue(out, doc);
        return out.toByteArray();
    }

    private static Map<String, Object> basiclitInt(long value) {
        return node("basiclit", mapOf("value", Long.toString(value), "type", 0L));
    }

    private static MakeNode findMake(NodeFactoryV2.V2Program prog) {
        for (NodeFactoryV2.V2Function fn : prog.functions()) {
            GoStmtNode body = fn.body();
            if (body == null) {
                continue;
            }
            Deque<Node> q = new ArrayDeque<>();
            q.add(body);
            while (!q.isEmpty()) {
                Node n = q.removeFirst();
                if (n instanceof MakeNode mn) {
                    return mn;
                }
                for (Node c : n.getChildren()) {
                    q.add(c);
                }
            }
        }
        return null;
    }

    private static NodeFactoryV2.V2Program build(byte[] doc) throws IOException {
        Source src = Source.newBuilder("go", "", "NodeFactoryV2MakeTest").build();
        return new NodeFactoryV2(src).buildProgram(doc);
    }

    private static FrontendException expectFrontendException(byte[] doc, String what) {
        try {
            build(doc);
        } catch (FrontendException e) {
            return e;
        } catch (IOException e) {
            throw new AssertionError("unexpected IOException: " + e.getMessage());
        }
        throw new AssertionError("expected FrontendException for " + what + ", build succeeded");
    }

    /** L18 core: make(chan T) must fail loudly, never build a slice node. */
    public void testMakeChanFailsLoudly() throws IOException {
        byte[] doc = makeDoc("chan", mapOf("dir", "both", "elem", 0L), new ArrayList<>());
        FrontendException e = expectFrontendException(doc, "make(chan int)");
        check(e.getMessage().contains("chan"),
                "error must name the chan kind, got: " + e.getMessage());
        check(e.getMessage().contains("Phase 2"),
                "error must name the Phase-2 scope, got: " + e.getMessage());
    }

    /** make(map[K]V) still builds a map node. */
    public void testMakeMapBuildsMapNode() throws IOException {
        byte[] doc = makeDoc("map", mapOf("key", 0L, "elem", 0L), new ArrayList<>());
        NodeFactoryV2.V2Program prog = build(doc);
        MakeNode mn = findMake(prog);
        check(mn != null, "expected a MakeNode in the built AST");
        check(mn.kind() == GoKind.MAP, "expected MAP kind, got " + mn.kind());
    }

    /** make([]T, n) still builds a slice node. */
    public void testMakeSliceBuildsSliceNode() throws IOException {
        byte[] doc = makeDoc("slice", mapOf("elem", 0L), List.of(basiclitInt(3)));
        NodeFactoryV2.V2Program prog = build(doc);
        MakeNode mn = findMake(prog);
        check(mn != null, "expected a MakeNode in the built AST");
        check(mn.kind() == GoKind.SLICE, "expected SLICE kind, got " + mn.kind());
    }

    /** Any other kind (e.g. array) must fail loudly too — no silent fall-through. */
    public void testMakeOtherKindFailsLoudly() throws IOException {
        byte[] doc = makeDoc("array", mapOf("len", 3L, "elem", 0L), new ArrayList<>());
        expectFrontendException(doc, "make([3]int)");
    }
}
