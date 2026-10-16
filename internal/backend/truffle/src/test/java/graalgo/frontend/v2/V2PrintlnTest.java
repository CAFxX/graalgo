package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.GoRootNode;
import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * V2-20: the {@code print}/{@code println} builtins in the v2 factory write
 * gc-compatible bytes to stderr.
 *
 * <p>Regression test: the factory used to evaluate the args and return the
 * first one, silently dropping the stderr write entirely. The programs here
 * are built through the real {@link NodeFactoryV2#buildProgram(byte[])} path
 * from a hand-encoded v2 binary document (the encoder below mirrors the wire
 * contract pinned by {@link BinaryDecoderTest}'s goldens), then executed on
 * a manually created frame with stderr captured.
 *
 * <p>Expected bytes follow gc's println semantics (a space between every
 * pair of operands, trailing newline) and print's (no separators, no
 * trailing newline).
 */
public final class V2PrintlnTest {

    // Type-table ids used by the literal helpers below.
    private static final long T_INT = 0;
    private static final long T_STRING = 1;
    private static final long T_BOOL = 2;

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    // --- tiny v2-binary encoder (test-side; mirrors BinaryDecoder's contract) ---

    private static void writeVarint(ByteArrayOutputStream out, long n) {
        while (true) {
            int b = (int) (n & 0x7F);
            n >>>= 7;
            if (n != 0) {
                b |= 0x80;
            }
            out.write(b);
            if (n == 0) {
                return;
            }
        }
    }

    @SuppressWarnings("unchecked")
    private static void encodeValue(ByteArrayOutputStream out, Object v) {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof Boolean b) {
            out.write(0x01);
            out.write(b ? 1 : 0);
        } else if (v instanceof Long l) {
            out.write(0x02);
            writeVarint(out, (l << 1) ^ (l >> 63)); // zigzag
        } else if (v instanceof String s) {
            byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
            out.write(0x03);
            writeVarint(out, bytes.length);
            out.write(bytes, 0, bytes.length);
        } else if (v instanceof List<?> list) {
            out.write(0x04);
            writeVarint(out, list.size());
            for (Object e : list) {
                encodeValue(out, e);
            }
        } else if (v instanceof Map<?, ?> map) {
            out.write(0x05);
            writeVarint(out, map.size());
            for (Map.Entry<?, ?> e : ((Map<String, Object>) map).entrySet()) {
                byte[] key = ((String) e.getKey()).getBytes(StandardCharsets.UTF_8);
                writeVarint(out, key.length);
                out.write(key, 0, key.length);
                encodeValue(out, e.getValue());
            }
        } else {
            throw new AssertionError("test encoder: unsupported value " + v);
        }
    }

    private static Map<String, Object> node(String kind, Map<String, Object> fields) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("kind", kind);
        m.put("fields", fields);
        return m;
    }

    private static Map<String, Object> basicLit(long typeId, String value) {
        Map<String, Object> fields = new LinkedHashMap<>();
        fields.put("value", value);
        fields.put("type", typeId);
        return node("basiclit", fields);
    }

    /** Builds a document for: {@code func main() { <builtin>(args...) }}. */
    private static byte[] encodeMain(String builtin, List<Map<String, Object>> args) {
        Map<String, Object> callFields = new LinkedHashMap<>();
        callFields.put("builtin", builtin);
        callFields.put("args", new ArrayList<>(args));
        Map<String, Object> call = node("call", callFields);

        Map<String, Object> exprStmtFields = new LinkedHashMap<>();
        exprStmtFields.put("x", call);
        Map<String, Object> exprStmt = node("exprstmt", exprStmtFields);

        Map<String, Object> blockFields = new LinkedHashMap<>();
        blockFields.put("stmts", List.of(exprStmt));
        Map<String, Object> block = node("block", blockFields);

        Map<String, Object> fnFields = new LinkedHashMap<>();
        fnFields.put("name", "main");
        fnFields.put("params", List.of());
        fnFields.put("body", block);

        Map<String, Object> intType = new LinkedHashMap<>();
        intType.put("kind", "basic");
        intType.put("detail", Map.of("name", "int", "class", "int"));
        Map<String, Object> stringType = new LinkedHashMap<>();
        stringType.put("kind", "basic");
        stringType.put("detail", Map.of("name", "string", "class", "string"));
        Map<String, Object> boolType = new LinkedHashMap<>();
        boolType.put("kind", "basic");
        boolType.put("detail", Map.of("name", "bool", "class", "bool"));

        Map<String, Object> doc = new LinkedHashMap<>();
        doc.put("frontend", "v2");
        doc.put("package", Map.of("name", "main"));
        doc.put("types", List.of(intType, stringType, boolType));
        doc.put("objects", List.of());
        doc.put("funcs", List.of(node("funcdecl", fnFields)));

        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write('g');
        out.write('2');
        out.write('b');
        out.write('i');
        writeVarint(out, 1); // format version
        encodeValue(out, doc);
        return out.toByteArray();
    }

    /**
     * Builds {@code func main() { <builtin>(args...) }} via the v2 factory,
     * executes it, and returns the exact bytes written to stderr.
     */
    private static byte[] runBuiltinToStderr(String builtin, List<Map<String, Object>> args)
            throws Exception {
        byte[] doc = encodeMain(builtin, args);
        NodeFactoryV2 factory = new NodeFactoryV2(
                Source.newBuilder("go", "", "v2println.go").build());
        NodeFactoryV2.V2Program prog = factory.buildProgram(doc);
        NodeFactoryV2.V2Function main = null;
        for (NodeFactoryV2.V2Function f : prog.functions()) {
            if ("main".equals(f.name())) {
                main = f;
            }
        }
        check(main != null, "main not built");
        check(main.hasBody(), "main has no body");

        // Warm up the Truffle runtime BEFORE capturing stderr: runtime init
        // must not pollute the captured bytes.
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], main.frameDescriptor());
        GoRootNode root = new GoRootNode(main.frameDescriptor(), main.body());

        PrintStream oldErr = System.err;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(buf, true, StandardCharsets.UTF_8));
        try {
            root.execute(frame);
        } finally {
            System.setErr(oldErr);
        }
        return buf.toByteArray();
    }

    private static void checkBytes(byte[] actual, byte[] expected, String what) {
        if (!Arrays.equals(actual, expected)) {
            throw new AssertionError(what + ": stderr bytes differ\n  expected: "
                    + Arrays.toString(expected) + "\n  actual:   " + Arrays.toString(actual));
        }
    }

    public void testV2PrintlnWritesToStderr() throws Exception {
        byte[] err = runBuiltinToStderr("println", List.of(
                basicLit(T_INT, "42"),
                basicLit(T_STRING, "\"hi\"")));
        // gc: println(a, b) ≡ print(a, " ", b, "\n") to stderr.
        checkBytes(err, "42 hi\n".getBytes(StandardCharsets.UTF_8), "println(42, \"hi\")");
    }

    public void testV2PrintlnEmptyIsNewline() throws Exception {
        byte[] err = runBuiltinToStderr("println", List.of());
        // gc: println() writes a bare newline.
        checkBytes(err, "\n".getBytes(StandardCharsets.UTF_8), "println()");
    }

    public void testV2PrintlnMixedKinds() throws Exception {
        byte[] err = runBuiltinToStderr("println", List.of(
                basicLit(T_BOOL, "true"),
                basicLit(T_INT, "-7"),
                basicLit(T_STRING, "\"x\"")));
        // gc: one space between every pair of operands, trailing newline.
        checkBytes(err, "true -7 x\n".getBytes(StandardCharsets.UTF_8),
                "println(true, -7, \"x\")");
    }

    public void testV2PrintNoSeparators() throws Exception {
        byte[] err = runBuiltinToStderr("print", List.of(
                basicLit(T_INT, "42"),
                basicLit(T_STRING, "\"hi\"")));
        // print emits no separators and no trailing newline.
        checkBytes(err, "42hi".getBytes(StandardCharsets.UTF_8), "print(42, \"hi\")");
    }

    public void testV2PrintlnArgSideEffectsEvaluated() throws Exception {
        // Both println args must be evaluated (in order); the old v2 stub
        // returned only the first arg and wrote nothing at all.
        byte[] err = runBuiltinToStderr("println", List.of(
                basicLit(T_INT, "1"),
                basicLit(T_INT, "2"),
                basicLit(T_INT, "3")));
        checkBytes(err, "1 2 3\n".getBytes(StandardCharsets.UTF_8), "println(1, 2, 3)");
    }
}
