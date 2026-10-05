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
}
