package graalgo.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * M2 launcher tests: the real frontend path end to end.
 *
 * <p>{@code --check} is the diffharness build step — it must succeed exactly
 * when gofrontend accepts the program and fail deterministically (exit 2,
 * gofrontend's message on stderr) when it rejects it. Run mode executes
 * {@code func main} via the Truffle AST. All tests shell out to the real
 * {@code gofrontend} binary resolved by {@link ToolPaths}.
 */
// verifies: GG-0027 (M2: launcher executes real programs end-to-end on the
// pinned toolchain; the GG-0027 pin-recorded-and-stable slice is exercised
// by every test below).
public final class MainTest {

    private static final Path CORPUS = Path.of("corpus", "straightline");

    private static String captureStderr(java.util.concurrent.Callable<Integer> r) throws Exception {
        PrintStream old = System.err;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(buf, true, StandardCharsets.UTF_8));
        try {
            r.call();
        } finally {
            System.setErr(old);
        }
        return buf.toString(StandardCharsets.UTF_8);
    }

    private static void assertEq(int expected, int actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }

    private static void assertContains(String haystack, String needle, String what) {
        if (!haystack.contains(needle)) {
            throw new AssertionError(what + ": expected stderr to contain <" + needle + "> but got <"
                    + haystack + ">");
        }
    }

    private static Path writeTempGo(String body) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-main-test");
        Path f = dir.resolve("prog.go");
        Files.writeString(f, "package main\n\nfunc main() {\n" + body + "}\n", StandardCharsets.UTF_8);
        return f;
    }

    public void testCheckAcceptsStraightLine() {
        assertEq(Main.EXIT_OK,
                Main.run(new String[] {"--check", CORPUS.resolve("sl01_consts.go").toString()}),
                "--check on a valid straight-line program");
    }

    public void testCheckRejectsTypeError() throws Exception {
        Path bad = writeTempGo("var x int = \"nope\"\n");
        final int[] code = new int[1];
        String err = captureStderr(() -> {
            code[0] = Main.run(new String[] {"--check", bad.toString()});
            return code[0];
        });
        assertEq(Main.EXIT_BUILD_FAILURE, code[0], "--check exit on type error");
        assertContains(err, "type error", "--check stderr on type error");
    }

    public void testCheckRejectsScopeError() throws Exception {
        Path bad = writeTempGo("select {\n}\n");
        final int[] code = new int[1];
        String err = captureStderr(() -> {
            code[0] = Main.run(new String[] {"--check", bad.toString()});
            return code[0];
        });
        assertEq(Main.EXIT_BUILD_FAILURE, code[0], "--check exit on out-of-scope stmt");
        assertContains(err, "WS E", "--check stderr names the owning workstream");
    }

    public void testCheckRejectsSyntaxError() throws Exception {
        Path bad = writeTempGo("var x int = \n");
        final int[] code = new int[1];
        String err = captureStderr(() -> {
            code[0] = Main.run(new String[] {"--check", bad.toString()});
            return code[0];
        });
        assertEq(Main.EXIT_BUILD_FAILURE, code[0], "--check exit on syntax error");
        assertContains(err, "parse error", "--check stderr on syntax error");
    }

    public void testCheckRejectsVariadicPacking() throws Exception {
        // Remediation (R1 P1-1, R2 P1-2): non-spread calls to variadic
        // functions need argument-packing adaptation that v1 does not
        // implement; the exporter must fail fast with a WS D scope error
        // instead of emitting a program that crashes at runtime with an
        // internal ArrayIndexOutOfBoundsException. Pre-fix, --check
        // accepted this program (exit 0).
        Path dir = Files.createTempDirectory("graalgo-main-test");
        Path f = dir.resolve("prog.go");
        Files.writeString(f,
                "package main\n\nfunc sum(nums ...int) int {\n\ttotal := 0\n" +
                "\tfor _, n := range nums {\n\t\ttotal += n\n\t}\n\treturn total\n}\n\n" +
                "func main() {\n\tprintln(sum(1, 2))\n}\n",
                StandardCharsets.UTF_8);
        final int[] code = new int[1];
        String err = captureStderr(() -> {
            code[0] = Main.run(new String[] {"--check", f.toString()});
            return code[0];
        });
        assertEq(Main.EXIT_BUILD_FAILURE, code[0], "--check exit on variadic packing call");
        assertContains(err, "variadic", "--check stderr names the variadic boundary");
        assertContains(err, "WS D", "--check stderr names the owning workstream");
    }

    public void testRunExecutesStraightLine() {
        assertEq(Main.EXIT_OK,
                Main.run(new String[] {CORPUS.resolve("sl02_arith.go").toString()}),
                "run on a valid straight-line program");
    }

    public void testRunIsSilent() throws Exception {
        PrintStream old = System.out;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(buf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = Main.run(new String[] {CORPUS.resolve("sl02_arith.go").toString()});
        } finally {
            System.setOut(old);
        }
        assertEq(Main.EXIT_OK, code, "run exit code");
        if (!buf.toString(StandardCharsets.UTF_8).isEmpty()) {
            throw new AssertionError("run must not print: got <"
                    + buf.toString(StandardCharsets.UTF_8) + ">");
        }
    }

    public void testMissingFile() throws Exception {
        final int[] code = new int[1];
        captureStderr(() -> {
            code[0] = Main.run(new String[] {"--check", "does/not/exist.go"});
            return code[0];
        });
        assertEq(Main.EXIT_INPUT_ERROR, code[0], "--check on missing file");
    }

    public void testUnknownFlag() throws Exception {
        final int[] code = new int[1];
        captureStderr(() -> {
            code[0] = Main.run(new String[] {"--frobnicate"});
            return code[0];
        });
        assertEq(Main.EXIT_INPUT_ERROR, code[0], "unknown flag");
    }

    public void testNoInputFile() throws Exception {
        final int[] code = new int[1];
        captureStderr(() -> {
            code[0] = Main.run(new String[] {"--check"});
            return code[0];
        });
        assertEq(Main.EXIT_INPUT_ERROR, code[0], "--check without a file");
    }

    public void testDumpVars() throws Exception {
        PrintStream old = System.out;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(buf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = Main.run(new String[] {"--dump-vars", CORPUS.resolve("sl01_consts.go").toString()});
        } finally {
            System.setOut(old);
        }
        assertEq(Main.EXIT_OK, code, "--dump-vars exit code");
        String out = buf.toString(StandardCharsets.UTF_8);
        // sl01: a=0xFF=255, b=0o17=15, c=0b101=5, d=-42, e=1000000, f=sum.
        assertContains(out, "a=255", "--dump-vars a");
        assertContains(out, "d=-42", "--dump-vars d");
        assertContains(out, "f=1000233", "--dump-vars f");
    }

    public void testRunNonAsciiSource() throws Exception {
        // M8 fuzzer find (P0): any multi-byte UTF-8 in a statement's source
        // range crashed NodeFactory with IllegalArgumentException
        // ("charIndex out of range") because the exporter sent byte-based
        // col/len while Truffle's createSection is char-based. Pre-fix, this
        // program died building the AST; post-fix it runs and prints exactly.
        Path dir = Files.createTempDirectory("graalgo-main-test");
        Path f = dir.resolve("prog.go");
        Files.writeString(f,
                "package main\n\nimport \"fmt\"\n\nfunc main() {\n" +
                "\tfmt.Println(\"日本語\")\n" +
                "\tfmt.Println(\"héllo wörld\")\n" +
                "\ts := \"日本語\" + \"!\"\n" +
                "\tfmt.Println(s, len(s))\n" +
                "}\n",
                StandardCharsets.UTF_8);
        PrintStream old = System.out;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(buf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = Main.run(new String[] {f.toString()});
        } finally {
            System.setOut(old);
        }
        assertEq(Main.EXIT_OK, code, "run exit on non-ASCII source");
        String out = buf.toString(StandardCharsets.UTF_8);
        // len("日本語") = 9 bytes; len("日本語!") = 10 bytes.
        String expected = "日本語\n" + "héllo wörld\n" + "日本語! 10\n";
        if (!expected.equals(out)) {
            throw new AssertionError("stdout on non-ASCII source: expected <"
                    + expected + "> but got <" + out + ">");
        }
    }

    public void testCheckRejectsClearBuiltin() throws Exception {
        // M8 fuzzer find (P2): clear/min/max are outside the v1 builtin
        // surface but the exporter emitted them, and NodeFactory died with
        // "internal error building AST". The exporter must fail fast with a
        // clean WS G scope error instead. Pre-fix, stderr said
        // "not implemented in NodeFactory v1", not "not supported in
        // frontend/v1 scope".
        for (String prog : new String[] {
                "m := map[string]int{\"a\": 1}\n\tclear(m)\n",
                "println(min(1, 2), max(3, 4))\n"}) {
            Path bad = writeTempGo(prog);
            final int[] code = new int[1];
            String err = captureStderr(() -> {
                code[0] = Main.run(new String[] {"--check", bad.toString()});
                return code[0];
            });
            assertEq(Main.EXIT_BUILD_FAILURE, code[0], "--check exit on " + prog.trim());
            assertContains(err, "not supported in frontend/v1 scope",
                    "--check stderr is a clean scope error for " + prog.trim());
            assertContains(err, "WS G", "--check stderr names WS G for " + prog.trim());
        }
    }

    public void testFmtInvalidUtf8Bytes() throws Exception {
        // M8 fuzzer find: Go strings are bytes. fmt of invalid UTF-8 must
        // emit raw bytes (never U+FFFD). Pre-fix, Fmt decoded via
        // new String(bytes, UTF_8) which sanitizes malformed input, so
        // Println("a\xff\xfe") printed "a\uFFFD\uFFFD".
        Path dir = Files.createTempDirectory("graalgo-main-test");
        Path f = dir.resolve("prog.go");
        byte[] prog = ("package main\n\nimport \"fmt\"\n\nfunc main() {\n" +
                "\tfmt.Println(\"a\" + \"\\xff\\xfe\")\n" +
                "\tfmt.Printf(\"%s|%v|%x|%q\\n\", \"a\"+\"\\xff\", \"b\"+\"\\xfe\", \"c\"+\"\\xff\", \"d\"+\"\\xfe\")\n" +
                "\tfmt.Printf(\"%q\\n\", []byte{0xff, 0xfe})\n" +
                "}\n").getBytes(StandardCharsets.UTF_8);
        Files.write(f, prog);
        PrintStream old = System.out;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(buf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = Main.run(new String[] {f.toString()});
        } finally {
            System.setOut(old);
        }
        assertEq(Main.EXIT_OK, code, "run exit on invalid-UTF8 program");
        byte[] got = buf.toByteArray();
        // gc: a\xff\xfe \n, then a\xff | b\xfe | 63ff | "d\xfe" \n,
        // then "\xff\xfe" (quoted) \n
        byte[] expected = new byte[] {
                'a', (byte) 0xFF, (byte) 0xFE, '\n',
                'a', (byte) 0xFF, '|',
                'b', (byte) 0xFE, '|',
                '6', '3', 'f', 'f', '|',
                '"', 'd', '\\', 'x', 'f', 'e', '"', '\n',
                '"', '\\', 'x', 'f', 'f', '\\', 'x', 'f', 'e', '"', '\n',
        };
        if (!java.util.Arrays.equals(expected, got)) {
            throw new AssertionError("stdout bytes on invalid UTF-8: expected <"
                    + hex(expected) + "> but got <" + hex(got) + ">");
        }
    }

    public void testFmtPointerStructValue() throws Exception {
        // M8 fuzzer find: Println(&T{...}) printed "0x0" because the
        // NodeFactory fmtcall metadata loop had no pointer branch, so
        // elemKind was null and fmtPointer fell through to erased output.
        // gc prints &{1 2} (the referent's %v with & prefix).
        Path dir = Files.createTempDirectory("graalgo-main-test");
        Path f = dir.resolve("prog.go");
        Files.writeString(f,
                "package main\n\nimport \"fmt\"\n\n" +
                "type fzPair struct{ X, Y int }\n\n" +
                "func main() {\n" +
                "\tp := &fzPair{1, 2}\n" +
                "\tfmt.Println(p)\n" +
                "\tfmt.Printf(\"%v %T\\n\", p, p)\n" +
                "}\n",
                StandardCharsets.UTF_8);
        PrintStream old = System.out;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(buf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = Main.run(new String[] {f.toString()});
        } finally {
            System.setOut(old);
        }
        assertEq(Main.EXIT_OK, code, "run exit on pointer-%v program");
        String out = buf.toString(StandardCharsets.UTF_8);
        String expected = "&{1 2}\n&{1 2} *main.fzPair\n";
        if (!expected.equals(out)) {
            throw new AssertionError("stdout on pointer %v: expected <"
                    + expected + "> but got <" + out + ">");
        }
    }

    private static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder();
        for (byte x : b) {
            sb.append(String.format("%02x ", x));
        }
        return sb.toString().trim();
    }

    /**
     * M9 (WS L): {@code --dump-polymorphism=path} records per-call-site target
     * cardinality. The {@code f(v)} site inside {@code apply} is invoked with
     * two different closures → bimorphic (targets 2); the two {@code apply}
     * call sites are monomorphic.
     */
    public void testDumpPolymorphism() throws Exception {
        Path dir = Files.createTempDirectory("graalgo-poly-test");
        Path f = dir.resolve("prog.go");
        Files.writeString(f,
                "package main\n\n" +
                "func apply(f func(int) int, v int) int { return f(v) }\n\n" +
                "func main() {\n" +
                "\ta := func(x int) int { return x + 1 }\n" +
                "\tb := func(x int) int { return x * 2 }\n" +
                "\tprintln(apply(a, 10))\n" +
                "\tprintln(apply(b, 10))\n" +
                "}\n",
                StandardCharsets.UTF_8);
        Path snap = dir.resolve("poly.json");
        int code;
        try {
            code = Main.run(new String[] {"--dump-polymorphism=" + snap, f.toString()});
        } finally {
            graalgo.values.CallSiteProbes.setEnabled(false);
            graalgo.values.CallSiteProbes.reset();
        }
        assertEq(Main.EXIT_OK, code, "run exit with --dump-polymorphism");
        String json = Files.readString(snap, StandardCharsets.UTF_8);
        if (!json.contains("\"targets\":2")) {
            throw new AssertionError(
                    "expected a bimorphic site (targets 2) in snapshot, got: " + json);
        }
    }

    /**
     * M9 (WS L), stderr half: the program under test uses the builtin
     * {@code println}, which writes to stderr (gc-accurate). Verifies the
     * program itself ran correctly with probing enabled.
     */
    public void testDumpPolymorphismProgramOutput() throws Exception {
        Path dir = Files.createTempDirectory("graalgo-poly-test");
        Path f = dir.resolve("prog.go");
        Files.writeString(f,
                "package main\n\n" +
                "func apply(f func(int) int, v int) int { return f(v) }\n\n" +
                "func main() {\n" +
                "\ta := func(x int) int { return x + 1 }\n" +
                "\tb := func(x int) int { return x * 2 }\n" +
                "\tprintln(apply(a, 10))\n" +
                "\tprintln(apply(b, 10))\n" +
                "}\n",
                StandardCharsets.UTF_8);
        Path snap = dir.resolve("poly.json");
        PrintStream old = System.err;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(buf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = Main.run(new String[] {"--dump-polymorphism=" + snap, f.toString()});
        } finally {
            System.setErr(old);
            graalgo.values.CallSiteProbes.setEnabled(false);
            graalgo.values.CallSiteProbes.reset();
        }
        assertEq(Main.EXIT_OK, code, "run exit with --dump-polymorphism");
        String err = buf.toString(StandardCharsets.UTF_8);
        assertContains(err, "11\n20\n", "program output with probing enabled");
    }

    /**
     * M9 (review #2 P1-6): the probe must record the compiled
     * {@code RootCallTarget} identity, not the {@code GoFunc} wrapper.
     * Each loop iteration allocates a fresh wrapper around the same
     * funclit target; the {@code f(v)} site must report targets:1. If the
     * probe recorded wrapper identity, this would be targets:2 — the test
     * is red under that bug, green under the correct implementation.
     */
    public void testDumpPolymorphismClosureIdentity() throws Exception {
        Path dir = Files.createTempDirectory("graalgo-poly-test");
        Path f = dir.resolve("prog.go");
        Files.writeString(f,
                "package main\n\n" +
                "func apply(f func(int) int, v int) int { return f(v) }\n\n" +
                "func main() {\n" +
                "\tfor i := 0; i < 2; i++ {\n" +
                "\t\tf := func(x int) int { return x + i }\n" +
                "\t\tprintln(apply(f, 10))\n" +
                "\t}\n" +
                "}\n",
                StandardCharsets.UTF_8);
        Path snap = dir.resolve("poly.json");
        PrintStream old = System.err;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(buf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = Main.run(new String[] {"--dump-polymorphism=" + snap, f.toString()});
        } finally {
            System.setErr(old);
            graalgo.values.CallSiteProbes.setEnabled(false);
            graalgo.values.CallSiteProbes.reset();
        }
        assertEq(Main.EXIT_OK, code, "run exit with --dump-polymorphism");
        String json = Files.readString(snap, StandardCharsets.UTF_8);
        // The f(v) site saw two distinct wrappers of one target: must be 1.
        // (The apply call sites are monomorphic; no site may exceed 1.)
        if (json.contains("\"targets\":2") || json.contains("\"targets\":3")) {
            throw new AssertionError(
                    "closure-allocating loop reported >1 target (wrapper identity leak): " + json);
        }
        if (!json.contains("\"targets\":1")) {
            throw new AssertionError(
                    "expected a monomorphic site in snapshot, got: " + json);
        }
        // Sanity: the program ran correctly (10, 11).
        String err = buf.toString(StandardCharsets.UTF_8);
        assertContains(err, "10\n11\n", "loop closure program output");
    }
}
