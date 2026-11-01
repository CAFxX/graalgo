package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * v2 print/println regression tests (review G3, 2026-11-01).
 *
 * <p>{@code NodeFactoryV2} used to return {@code args.get(0)} (or a
 * {@code NilNode} for zero args) for {@code print}/{@code println}:
 * no output was produced and arguments 2..n were never evaluated,
 * despite the comment claiming side-effect evaluation. These tests
 * build a real v2 binary document, run it through
 * {@link NodeFactoryV2#buildProgram}, execute the built body, and
 * assert the exact stderr bytes.
 *
 * <p>The documents are encoded here with a minimal writer mirroring
 * {@link BinaryDecoder} (magic "g2bi", version 1, zigzag varints);
 * they use only node shapes the Go emitter produces (basiclit/call/
 * exprstmt/block/funcdecl), except for {@link #testNestedPrintArgIsEvaluatedInOrder},
 * which is a factory-level probe: it nests a {@code print} call as an
 * argument to prove every argument is evaluated, in order. That nesting
 * is not a Go program the emitter would produce, but the factory's
 * contract is to evaluate all arguments of a builtin call, and the
 * probe makes a dropped argument observable.
 */
public final class PrintBuiltinV2Test {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    // --- Minimal v2 binary writer (mirror of BinaryDecoder) ---

    private static void varint(ByteArrayOutputStream out, long v) {
        long x = v;
        while (true) {
            int b = (int) (x & 0x7F);
            x >>>= 7;
            if (x == 0) {
                out.write(b);
                return;
            }
            out.write(b | 0x80);
        }
    }

    private static void bytes(ByteArrayOutputStream out, byte[] b) {
        varint(out, b.length);
        out.write(b, 0, b.length);
    }

    private static void value(ByteArrayOutputStream out, Object v) {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof Boolean b) {
            out.write(0x01);
            out.write(b ? 1 : 0);
        } else if (v instanceof Long l) {
            out.write(0x02);
            varint(out, (l << 1) ^ (l >> 63)); // zigzag
        } else if (v instanceof String s) {
            out.write(0x03);
            bytes(out, s.getBytes(StandardCharsets.UTF_8));
        } else if (v instanceof List<?> list) {
            out.write(0x04);
            varint(out, list.size());
            for (Object e : list) {
                value(out, e);
            }
        } else if (v instanceof Map<?, ?> map) {
            out.write(0x05);
            varint(out, map.size());
            for (Map.Entry<?, ?> e : map.entrySet()) {
                bytes(out, ((String) e.getKey()).getBytes(StandardCharsets.UTF_8));
                value(out, e.getValue());
            }
        } else {
            throw new AssertionError("unencodable value: " + v);
        }
    }

    private static byte[] document(Map<String, Object> doc) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write('g');
        out.write('2');
        out.write('b');
        out.write('i');
        varint(out, 1); // format version
        value(out, doc);
        return out.toByteArray();
    }

    // --- Document builders (shapes the Go emitter produces) ---

    private static Map<String, Object> map(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    /** Type table: id 0 = string, id 1 = int. */
    private static List<Object> types() {
        return List.of(
            map("kind", "basic",
                "detail", map("name", "string", "class", "string")),
            map("kind", "basic",
                "detail", map("name", "int", "class", "int")));
    }

    private static Map<String, Object> strLit(String s) {
        return map("kind", "basiclit",
            "fields", map("value", "\"" + s + "\"", "type", 0L));
    }

    private static Map<String, Object> intLit(long n) {
        return map("kind", "basiclit",
            "fields", map("value", Long.toString(n), "type", 1L));
    }

    private static Map<String, Object> builtinCall(String builtin, List<Object> args) {
        return map("kind", "call",
            "fields", map("builtin", builtin, "args", args));
    }

    /** A whole program whose main body is a single expression statement. */
    private static byte[] programWithStmt(Map<String, Object> stmt) {
        Map<String, Object> body = map("kind", "block",
            "fields", map("stmts", List.of(stmt)));
        Map<String, Object> fn = map("kind", "funcdecl",
            "fields", map("name", "main", "params", List.of(), "body", body));
        return document(map(
            "frontend", "v2",
            "types", types(),
            "objects", List.of(),
            "funcs", List.of(fn)));
    }

    private static Map<String, Object> exprStmt(Map<String, Object> x) {
        return map("kind", "exprstmt", "fields", map("x", x));
    }

    /** Builds the program, executes main's body, returns captured stderr. */
    private static String runMain(byte[] doc) throws IOException {
        NodeFactoryV2 factory = new NodeFactoryV2(
            Source.newBuilder("go", "package main", "print-v2.go").build());
        NodeFactoryV2.V2Program program = factory.buildProgram(doc);
        check(program.functionCount() == 1, "want 1 function, got " + program.functionCount());
        NodeFactoryV2.V2Function fn = program.functions().get(0);
        check("main".equals(fn.name()), "want main, got " + fn.name());
        check(fn.hasBody(), "main has no body");
        VirtualFrame frame = Truffle.getRuntime()
            .createVirtualFrame(new Object[0], fn.frameDescriptor());
        PrintStream savedErr = System.err;
        ByteArrayOutputStream captured = new ByteArrayOutputStream();
        System.setErr(new PrintStream(captured, true, StandardCharsets.UTF_8));
        try {
            fn.body().executeVoid(frame);
        } finally {
            System.setErr(savedErr);
        }
        return captured.toString(StandardCharsets.UTF_8);
    }

    private static void expect(String builtin, List<Object> args, String want) throws IOException {
        String got = runMain(programWithStmt(exprStmt(builtinCall(builtin, args))));
        check(want.equals(got),
            builtin + " output mismatch: want <" + want + "> got <" + got + ">");
    }

    /** println with several arguments: all must appear, space-separated, with newline. */
    public void testPrintlnMultipleArgs() throws IOException {
        expect("println", List.of(strLit("a"), intLit(1), strLit("b")), "a 1 b\n");
    }

    /** print with several string arguments: all must appear, no trailing newline. */
    public void testPrintMultipleArgs() throws IOException {
        expect("print", List.of(strLit("a"), strLit("b"), strLit("c")), "abc");
    }

    /** Zero-argument println must still emit its newline; zero-arg print emits nothing. */
    public void testZeroArgs() throws IOException {
        expect("println", List.of(), "\n");
        expect("print", List.of(), "");
    }

    /**
     * Side-effect evaluation order: the inner print must run (writing "x")
     * before the outer println formats its arguments, and the outer call's
     * later argument ("y") and trailing newline must not be dropped.
     * Before the G3 fix both levels collapsed to their first argument
     * (the inner print to its "x" literal, the outer to that), so the
     * output was empty.
     */
    public void testNestedPrintArgIsEvaluatedInOrder() throws IOException {
        Map<String, Object> inner = builtinCall("print", List.of(strLit("x")));
        expect("println", List.of(inner, strLit("y")), "x(0x0,0x0) y\n");
    }
}
