package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import graalgo.frontend.FrontendException;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * v2 print/println regression tests (REVIEW-2026-11-17-07 NEW-P1-1).
 *
 * <p>{@code NodeFactoryV2} used to return {@code args.get(0)} (or a
 * {@code NilNode} for zero args) for {@code print}/{@code println}:
 * no output was produced and arguments 2..n were never evaluated,
 * despite the comment claiming side-effect evaluation. These tests
 * build a real v2 binary document, run it through
 * {@link NodeFactoryV2#buildProgram}, execute the built body, and
 * assert the exact stderr bytes. Expected bytes are gc-verified
 * (go1.27.1 {@code go run} probes): {@code println(42,"hi")} →
 * {@code "42 hi\n"}, {@code print(1,2)} → {@code "12"} (no separator
 * for {@code print}), {@code println(true,-7,"x")} → {@code "true -7
 * x\n"}, {@code println(1,2,3)} → {@code "1 2 3\n"}.
 *
 * <p>The documents are encoded here with a minimal writer mirroring
 * {@link BinaryDecoder} (magic "g2bi", version 1, zigzag varints);
 * they use only node shapes the Go emitter produces (basiclit/call/
 * exprstmt/block/funcdecl). String literals carry the canonical
 * DECODED value (REVIEW-2026-11-06-10 R-P0-2), not quoted source
 * spelling.
 *
 * <p>{@link #testNestedPrintArgIsEvaluatedInOrder} and {@link
 * #testSecondArgSideEffectRuns} are factory-level probes: they nest
 * a {@code print} call as an argument to prove every argument is
 * evaluated, in order. That nesting is not a Go program the emitter
 * would produce (gc rejects print-as-value), but the factory's
 * contract is to evaluate all arguments of a builtin call, and the
 * probe makes a dropped argument observable. The inner call's
 * INTERFACE result formats as {@code (0x0,0x0)} (see
 * {@code BuiltinPrint}).
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

    /** Type table: id 0 = string, id 1 = int, id 2 = bool, id 3 = float64, id 4 = struct, id 5 = array. */
    private static List<Object> types() {
        return List.of(
            map("kind", "basic",
                "detail", map("name", "string", "class", "string")),
            map("kind", "basic",
                "detail", map("name", "int", "class", "int")),
            map("kind", "basic",
                "detail", map("name", "bool", "class", "bool")),
            map("kind", "basic",
                "detail", map("name", "float64", "class", "float")),
            map("kind", "struct",
                "detail", map("fields", List.of(
                    map("name", "X", "type", 1L)))),
            map("kind", "array",
                "detail", map("elem", 1L, "len", 2L)));
    }

    /** Canonical decoded string value (R-P0-2): no quotes, no unescaping. */
    private static Map<String, Object> strLit(String s) {
        return map("kind", "basiclit",
            "fields", map("value", s, "type", 0L));
    }

    private static Map<String, Object> intLit(long n) {
        return map("kind", "basiclit",
            "fields", map("value", Long.toString(n), "type", 1L));
    }

    private static Map<String, Object> boolLit(boolean b) {
        return map("kind", "basiclit",
            "fields", map("value", Boolean.toString(b), "type", 2L));
    }

    private static Map<String, Object> floatLit(double d) {
        return map("kind", "basiclit",
            "fields", map("value", Double.toString(d), "type", 3L));
    }

    /** Struct literal S{1}: composite literal of struct type id 4. */
    private static Map<String, Object> structLit() {
        return map("kind", "compositelit",
            "fields", map("type", 4L, "elems", List.of(intLit(1))));
    }

    /** Array literal [2]int{1, 2}: composite literal of array type id 5. */
    private static Map<String, Object> arrayLit() {
        return map("kind", "compositelit",
            "fields", map("type", 5L, "elems", List.of(intLit(1), intLit(2))));
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

    /** print with several arguments: all must appear, no separators, no trailing newline. */
    public void testPrintMultipleArgs() throws IOException {
        expect("print", List.of(strLit("a"), strLit("b"), strLit("c")), "abc");
        expect("print", List.of(intLit(42), strLit("hi")), "42hi");
        expect("print", List.of(intLit(1), intLit(2)), "12");
    }

    /** println per-type formats (gc-verified): bool, negative int, float, multi-int. */
    public void testPrintlnScalarFormats() throws IOException {
        expect("println", List.of(boolLit(true), intLit(-7), strLit("x")), "true -7 x\n");
        expect("println", List.of(intLit(1), intLit(2), intLit(3)), "1 2 3\n");
        expect("println", List.of(floatLit(1.5), strLit("x")), "1.5 x\n");
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
     * Before the fix the outer call returned its first argument, so the
     * output was just "x" (the inner call, executed standalone).
     */
    public void testNestedPrintArgIsEvaluatedInOrder() throws IOException {
        Map<String, Object> inner = builtinCall("print", List.of(strLit("x")));
        expect("println", List.of(inner, strLit("y")), "x(0x0,0x0) y\n");
    }

    /**
     * The side-effecting SECOND argument must run: the inner print writes
     * "x" while the outer println evaluates its arguments, and the outer
     * call must still print its first argument ("y"), the inner call's
     * INTERFACE result, and the trailing newline. Before the fix the
     * outer call returned args.get(0) ("y", a bare literal), so the
     * output was empty and the inner print never ran.
     */
    public void testSecondArgSideEffectRuns() throws IOException {
        Map<String, Object> inner = builtinCall("print", List.of(strLit("x")));
        expect("println", List.of(strLit("y"), inner), "xy (0x0,0x0)\n");
    }

    /** Building a program whose print/println takes a struct/array operand must fail. */
    private static void expectBuildRejected(byte[] doc, String name) throws IOException {
        NodeFactoryV2 factory = new NodeFactoryV2(
            Source.newBuilder("go", "package main", "print-v2.go").build());
        try {
            factory.buildProgram(doc);
        } catch (FrontendException e) {
            check(e.getMessage().contains("badtype"),
                name + " rejected, wrong message: " + e.getMessage());
            return;
        }
        throw new AssertionError(name + ": build succeeded, want FrontendException");
    }

    /**
     * gc rejects struct/array print operands at COMPILE time (badtype in
     * walkPrint); the factory must fail the build, not throw
     * IllegalArgumentException from BuiltinPrint at runtime.
     */
    public void testPrintlnStructRejected() throws IOException {
        expectBuildRejected(
            programWithStmt(exprStmt(builtinCall("println", List.of(structLit())))),
            "println(struct)");
    }

    /** Same guard for print (not just println) and for array operands. */
    public void testPrintArrayRejected() throws IOException {
        expectBuildRejected(
            programWithStmt(exprStmt(builtinCall("print", List.of(arrayLit())))),
            "print(array)");
    }
}
