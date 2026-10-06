package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * End-to-end tests for the P3-M4 linkname surface (GG-0020, ADR-0004 D1):
 * the intrinsic→Go direction ({@code Linknames.callGo}) through a real
 * Go program, and the no-leak guarantee across program builds in one JVM.
 *
 * <p>These tests shell out to the real {@code gofrontend} binary (like
 * the corpus runners); they do not involve gc — the tested programs use
 * graalgo-specific linkname symbols that gc cannot link.
 *
 * <p>verifies: GG-0020
 */
public final class LinknameEndToEndTest {

    private static final String CALLGO_PREAMBLE =
            "package main\n\nimport (\n\t\"fmt\"\n\t_ \"unsafe\"\n)\n\n"
            + "//go:linkname callGo graalgo.runtime.Linknames.callGo\n"
            + "func callGo(name string, arg any) any\n\n";

    private static final String PROG_HELPER =
            CALLGO_PREAMBLE
            + "func helper(x int64) int64 {\n\treturn x * 2\n}\n\n"
            + "func main() {\n\tfmt.Println(callGo(\"main.helper\", int64(21)).(int64))\n}\n";

    /** Same as PROG_HELPER but defines no helper: the call must fail. */
    private static final String PROG_NO_HELPER =
            CALLGO_PREAMBLE
            + "func main() {\n\tfmt.Println(callGo(\"main.helper\", int64(21)))\n}\n";

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-linkname-test");
        Path f = dir.resolve(name);
        Files.writeString(f, src, StandardCharsets.UTF_8);
        return f;
    }

    /** Runs one program through P3SyncMain, capturing stdout+stderr. */
    private static RunResult runProgram(Path goFile) {
        PrintStream oldOut = System.out;
        PrintStream oldErr = System.err;
        ByteArrayOutputStream outBuf = new ByteArrayOutputStream();
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(outBuf, true, StandardCharsets.UTF_8));
        System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = P3SyncMain.run(new String[] {"--gmp=1", goFile.toString()});
        } finally {
            System.setOut(oldOut);
            System.setErr(oldErr);
        }
        return new RunResult(code, outBuf.toString(StandardCharsets.UTF_8),
                errBuf.toString(StandardCharsets.UTF_8));
    }

    private record RunResult(int exit, String stdout, String stderr) {}

    /**
     * verifies: GG-0020 — P2-3: the intrinsic→Go direction end to end.
     * A Go program links {@code graalgo.runtime.Linknames.callGo} and
     * calls a Go helper through it with a concrete arg for an
     * any-typed parameter (exercises the GoFuncRootNode arg-unwrap
     * path, P2-5); prints 42.
     */
    public void testCallGoEndToEndPrints42() throws Exception {
        Path go = writeTempGo("cg42.go", PROG_HELPER);
        RunResult r = runProgram(go);
        assertTrue(r.exit() == 0,
                "expected exit 0, got " + r.exit() + "; stderr: " + r.stderr());
        assertTrue(r.stdout().trim().equals("42"),
                "expected stdout '42', got <" + r.stdout() + ">");
    }

    /**
     * verifies: GG-0020 — P2-2: no leak across program builds in one JVM.
     * After running a program that binds {@code main.helper}, running a
     * second program that does NOT define it must fail fast on the
     * unbound name — not silently reuse the first program's binding.
     */
    public void testNoLeakAcrossProgramsInOneJvm() throws Exception {
        Path a = writeTempGo("cgA.go", PROG_HELPER);
        RunResult ra = runProgram(a);
        assertTrue(ra.exit() == 0 && ra.stdout().trim().equals("42"),
                "program A should print 42, got exit=" + ra.exit()
                        + " stdout=<" + ra.stdout() + "> stderr=<" + ra.stderr() + ">");
        Path b = writeTempGo("cgB.go", PROG_NO_HELPER);
        RunResult rb = runProgram(b);
        assertTrue(rb.exit() != 0,
                "program B must fail (unbound main.helper), got exit 0 with stdout=<"
                        + rb.stdout() + ">");
        assertTrue(!rb.stdout().contains("42"),
                "program B must not print the stale binding's 42; stdout=<" + rb.stdout() + ">");
        assertTrue(rb.stderr().contains("main.helper"),
                "program B's error should name the unbound symbol; stderr=<" + rb.stderr() + ">");
    }
}
