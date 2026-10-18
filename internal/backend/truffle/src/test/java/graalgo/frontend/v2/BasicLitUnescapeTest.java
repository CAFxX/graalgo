package graalgo.frontend.v2;

import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.ConstNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoStmtNode;
import graalgo.values.GoString;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * L02: string/rune/int literals must decode to their true values.
 *
 * <p>The v2 wire format carries basic literals as raw source text in
 * "value". Go escape sequences, raw-string backquotes, and non-decimal int
 * spellings cannot be recovered from that text by quote-stripping — the old
 * Java code did exactly that and silently corrupted data ("a\nb" became 4
 * chars, '\n' became 92, `017` became 17). Format v2 fixes the contract:
 * the Go emitter ships the decoded value in "str" (via strconv.Unquote)
 * and the canonical decimal in "const" (via go/constant); the factory
 * consumes those fields and fails loudly when they are absent.
 *
 * <p>These tests build minimal v2 binary documents by hand (mirroring the
 * Go encoder's tags), run them through
 * {@link NodeFactoryV2#buildProgram(byte[])}, and assert the resulting
 * {@link ConstNode} values.
 */
public final class BasicLitUnescapeTest {

    // Type table ids used by the test documents.
    private static final long TYPE_STRING = 0;
    private static final long TYPE_INT = 1;
    private static final long TYPE_RUNE = 2;

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    // --- Minimal v2 binary encoder (mirrors encode.go tags) ---

    private static void writeVarint(ByteArrayOutputStream out, long v) {
        while ((v & ~0x7FL) != 0) {
            out.write((byte) ((v & 0x7F) | 0x80));
            v >>>= 7;
        }
        out.write((byte) v);
    }

    private static void writeBytes(ByteArrayOutputStream out, byte[] b) {
        writeVarint(out, b.length);
        out.write(b, 0, b.length);
    }

    @SuppressWarnings("unchecked")
    private static void writeValue(ByteArrayOutputStream out, Object v) {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof String s) {
            out.write(0x03);
            writeBytes(out, s.getBytes(StandardCharsets.UTF_8));
        } else if (v instanceof Long l) {
            out.write(0x02);
            writeVarint(out, (l << 1) ^ (l >> 63)); // zigzag
        } else if (v instanceof List<?> list) {
            out.write(0x04);
            writeVarint(out, list.size());
            for (Object e : list) {
                writeValue(out, e);
            }
        } else if (v instanceof Map<?, ?> map) {
            out.write(0x05);
            writeVarint(out, map.size());
            // The Go encoder sorts map keys; do the same.
            List<String> keys = new ArrayList<>();
            for (Object k : map.keySet()) {
                keys.add((String) k);
            }
            Collections.sort(keys);
            for (String k : keys) {
                writeBytes(out, k.getBytes(StandardCharsets.UTF_8));
                writeValue(out, ((Map<String, Object>) map).get(k));
            }
        } else {
            throw new AssertionError("unsupported test value: " + v);
        }
    }

    private static Map<String, Object> basicType(String name, String cls) {
        Map<String, Object> detail = new LinkedHashMap<>();
        detail.put("name", name);
        detail.put("class", cls);
        Map<String, Object> t = new LinkedHashMap<>();
        t.put("kind", "basic");
        t.put("detail", detail);
        return t;
    }

    private static Map<String, Object> basiclit(
            String value, long typeId, String str, String constVal) {
        Map<String, Object> fields = new LinkedHashMap<>();
        fields.put("value", value);
        fields.put("type", typeId);
        if (str != null) {
            fields.put("str", str);
        }
        if (constVal != null) {
            fields.put("const", constVal);
        }
        Map<String, Object> node = new LinkedHashMap<>();
        node.put("kind", "basiclit");
        node.put("fields", fields);
        return node;
    }

    private static Map<String, Object> funcWithLit(String name, Map<String, Object> lit) {
        Map<String, Object> exprStmt = new LinkedHashMap<>();
        exprStmt.put("kind", "exprstmt");
        Map<String, Object> xf = new LinkedHashMap<>();
        xf.put("x", lit);
        exprStmt.put("fields", xf);

        Map<String, Object> block = new LinkedHashMap<>();
        block.put("kind", "block");
        Map<String, Object> bf = new LinkedHashMap<>();
        bf.put("stmts", List.of(exprStmt));
        block.put("fields", bf);

        Map<String, Object> fn = new LinkedHashMap<>();
        fn.put("kind", "funcdecl");
        Map<String, Object> ff = new LinkedHashMap<>();
        ff.put("name", name);
        ff.put("params", new ArrayList<>());
        ff.put("body", block);
        fn.put("fields", ff);
        return fn;
    }

    private static byte[] docBytes(List<Map<String, Object>> funcs) {
        Map<String, Object> doc = new LinkedHashMap<>();
        doc.put("frontend", "v2");
        Map<String, Object> pkg = new LinkedHashMap<>();
        pkg.put("name", "main");
        doc.put("package", pkg);
        List<Object> types = new ArrayList<>();
        types.add(basicType("string", "string"));
        types.add(basicType("int", "int"));
        types.add(basicType("rune", "int"));
        doc.put("types", types);
        doc.put("objects", new ArrayList<>());
        doc.put("funcs", new ArrayList<>(funcs));
        doc.put("globals", new ArrayList<>());

        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write(new byte[] {'g', '2', 'b', 'i'}, 0, 4);
        writeVarint(out, 2); // format version 2
        writeValue(out, doc);
        return out.toByteArray();
    }

    private static NodeFactoryV2.V2Program build(List<Map<String, Object>> funcs) throws Exception {
        Source source = Source.newBuilder("go", "", "basiclit-test").build();
        return new NodeFactoryV2(source).buildProgram(docBytes(funcs));
    }

    private static Node singleChild(Node n, String what) {
        Iterator<Node> it = n.getChildren().iterator();
        check(it.hasNext(), what + ": expected a child node");
        Node c = it.next();
        check(!it.hasNext(), what + ": expected exactly one child node");
        return c;
    }

    /** The ConstNode built for funcs[i]'s single basiclit statement. */
    private static ConstNode soleConst(NodeFactoryV2.V2Program program, int funcIndex) {
        GoStmtNode body = program.functions().get(funcIndex).body();
        Node exprStmt = singleChild(body, "block");
        Node expr = singleChild(exprStmt, "exprstmt");
        check(expr instanceof ConstNode, "expected ConstNode, got " + expr.getClass());
        return (ConstNode) expr;
    }

    private static String constString(NodeFactoryV2.V2Program program, int funcIndex) {
        Object v = soleConst(program, funcIndex).executeObject(null);
        check(v instanceof GoString, "expected GoString, got " + v);
        return new String(((GoString) v).bytes(), StandardCharsets.UTF_8);
    }

    private static long constLong(NodeFactoryV2.V2Program program, int funcIndex) {
        return soleConst(program, funcIndex).executeLong(null);
    }

    // --- Positive cases ---

    public void testStringEscapesDecoded() throws Exception {
        // Wire: raw source text "a\nb" (backslash-n as two chars) plus the
        // decoded "str". The old code produced 4 chars a,\,n,b.
        Map<String, Object> lit =
            basiclit("\"a\\nb\"", TYPE_STRING, "a\nb", "\"a\\nb\"");
        NodeFactoryV2.V2Program program = build(List.of(funcWithLit("f0", lit)));
        check("a\nb".equals(constString(program, 0)),
            "string escapes not decoded, got: " + escape(constString(program, 0)));
    }

    public void testRawStringBackquotesStripped() throws Exception {
        // Raw strings keep backslashes verbatim but must lose the backquotes;
        // the old code kept the backquotes in the value.
        Map<String, Object> lit =
            basiclit("`raw\\nlit`", TYPE_STRING, "raw\\nlit", "\"raw\\\\nlit\"");
        NodeFactoryV2.V2Program program = build(List.of(funcWithLit("f0", lit)));
        check("raw\\nlit".equals(constString(program, 0)),
            "raw string misdecoded, got: " + escape(constString(program, 0)));
    }

    public void testStringUnicodeDecoded() throws Exception {
        Map<String, Object> lit =
            basiclit("\"caf\\u00e9\"", TYPE_STRING, "caf\u00e9", "\"caf\\u00e9\"");
        NodeFactoryV2.V2Program program = build(List.of(funcWithLit("f0", lit)));
        check("caf\u00e9".equals(constString(program, 0)),
            "unicode string misdecoded, got: " + escape(constString(program, 0)));
    }

    public void testRuneNewlineViaConst() throws Exception {
        // '\n': the canonical "const" (code point 10) is preferred; the old
        // code read 92 (backslash) via quote-stripping.
        Map<String, Object> lit = basiclit("'\\n'", TYPE_RUNE, "\n", "10");
        NodeFactoryV2.V2Program program = build(List.of(funcWithLit("f0", lit)));
        check(constLong(program, 0) == 10L,
            "rune '\\n' decoded as " + constLong(program, 0) + ", want 10");
    }

    public void testRuneHexViaStrFallback() throws Exception {
        // No "const" on the wire: the rune branch must decode from "str".
        // '\x41' stripped to "\x41" would read codePointAt(0) = 92.
        Map<String, Object> lit = basiclit("'\\x41'", TYPE_RUNE, "A", null);
        NodeFactoryV2.V2Program program = build(List.of(funcWithLit("f0", lit)));
        check(constLong(program, 0) == 65L,
            "rune '\\x41' decoded as " + constLong(program, 0) + ", want 65");
    }

    public void testLegacyOctalInt() throws Exception {
        // 017: the canonical "const" is decimal 15; the old code parsed the
        // raw text as decimal 17.
        Map<String, Object> lit = basiclit("017", TYPE_INT, null, "15");
        NodeFactoryV2.V2Program program = build(List.of(funcWithLit("f0", lit)));
        check(constLong(program, 0) == 15L,
            "017 decoded as " + constLong(program, 0) + ", want 15");
    }

    public void testHexInt() throws Exception {
        Map<String, Object> lit = basiclit("0x10", TYPE_INT, null, "16");
        NodeFactoryV2.V2Program program = build(List.of(funcWithLit("f0", lit)));
        check(constLong(program, 0) == 16L,
            "0x10 decoded as " + constLong(program, 0) + ", want 16");
    }

    // --- Negative cases: missing decoded fields must fail loudly ---

    public void testStringWithoutStrFailsLoudly() throws Exception {
        // A v1-style payload (no "str") must not be silently misdecoded.
        Map<String, Object> lit = basiclit("\"abc\"", TYPE_STRING, null, "\"abc\"");
        try {
            build(List.of(funcWithLit("f0", lit)));
        } catch (FrontendException e) {
            check(e.getMessage().contains("\"str\""),
                "error must name the missing field, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("string literal without \"str\" was not rejected");
    }

    public void testRuneWithoutStrOrConstFailsLoudly() throws Exception {
        Map<String, Object> lit = basiclit("'\\n'", TYPE_RUNE, null, null);
        try {
            build(List.of(funcWithLit("f0", lit)));
        } catch (FrontendException e) {
            check(e.getMessage().contains("\"str\""),
                "error must name the missing field, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("rune literal without \"str\"/\"const\" was not rejected");
    }

    private static String escape(String s) {
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '\n': sb.append("\\n"); break;
                case '\t': sb.append("\\t"); break;
                case '\\': sb.append("\\\\"); break;
                default:
                    if (c < 0x20 || c > 0x7E) {
                        sb.append(String.format("\\u%04x", (int) c));
                    } else {
                        sb.append(c);
                    }
            }
        }
        return sb.toString();
    }
}
