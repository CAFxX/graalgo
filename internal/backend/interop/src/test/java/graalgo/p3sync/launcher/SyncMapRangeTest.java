package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Regression tests for {@code sync.Map.Range} through the real P3SyncMain
 * stack (R2316-01-P1-1).
 *
 * <p>R2316-01-P1-1: {@code SyncOpNode.doRange} composed three individually
 * synchronized calls per key ({@code keysSnapshot} + {@code containsKey} +
 * {@code get}), so a {@code Delete} landing between the check and the read
 * handed the callback a nil value for an absent key — gc never invokes the
 * callback with nil for an absent key, it skips the entry. The fix is a
 * single atomic {@code load()} per key.
 *
 * <p>The concurrent test below is a smoke test only: concurrent
 * Range+Delete through the Go API must not crash, hang, or deliver nil.
 * It is NOT the regression test for the TOCTOU — the window between the
 * check and the read is a few JIT-compiled bytecodes, far too small for
 * the interpreter's deleter goroutine to hit reliably (measured: ~80
 * map ops/s; the stress observed {@code bad=0} pre-fix across 20,000+
 * windows). The regression test that does detect the defect is
 * {@code graalgo.p3sync.nodes.SyncOpNodeRangeTest}, which drives the
 * same {@code doRange} code with real Java threads.
 */
public final class SyncMapRangeTest {

    /** Range visits every stored entry with its stored value. */
    private static final String PROG_VISITS_ALL =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tvar m sync.Map\n"
            + "\tm.Store(1, 10)\n"
            + "\tm.Store(2, 20)\n"
            + "\tm.Store(3, 30)\n"
            + "\tn := 0\n"
            + "\tsum := 0\n"
            + "\tm.Range(func(k, v any) bool {\n"
            + "\t\tn++\n"
            + "\t\tsum += v.(int)\n"
            + "\t\treturn true\n"
            + "\t})\n"
            + "\tfmt.Println(n, sum)\n"
            + "}\n";

    /** Range skips keys deleted before the iteration starts. */
    private static final String PROG_SKIPS_DELETED =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tvar m sync.Map\n"
            + "\tfor i := 1; i <= 5; i++ {\n"
            + "\t\tm.Store(i, i*10)\n"
            + "\t}\n"
            + "\tm.Delete(2)\n"
            + "\tm.Delete(4)\n"
            + "\tn := 0\n"
            + "\tm.Range(func(k, v any) bool {\n"
            + "\t\tn++\n"
            + "\t\treturn true\n"
            + "\t})\n"
            + "\tfmt.Println(n)\n"
            + "}\n";

    /** Range stops when the callback returns false. */
    private static final String PROG_EARLY_STOP =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tvar m sync.Map\n"
            + "\tfor i := 1; i <= 5; i++ {\n"
            + "\t\tm.Store(i, i*10)\n"
            + "\t}\n"
            + "\tn := 0\n"
            + "\tm.Range(func(k, v any) bool {\n"
            + "\t\tn++\n"
            + "\t\treturn n < 2\n"
            + "\t})\n"
            + "\tfmt.Println(n)\n"
            + "}\n";

    /**
     * Smoke test: a deleter cycles delete-all/store-all over a few keys
     * while Range runs. All stored values are non-nil, so any nil value
     * reaching the callback would be the defect — but this test is not
     * sensitive to it (see the class comment): it guards against crashes
     * and hangs under concurrent use, and asserts the correct behaviour
     * (no nil) on the paths it does exercise.
     */
    private static final String PROG_CONCURRENT_DELETE =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tvar m sync.Map\n"
            + "\tconst N = 20\n"
            + "\tfor i := 0; i < N; i++ {\n"
            + "\t\tm.Store(i, i+1)\n"
            + "\t}\n"
            + "\tgo func() {\n"
            + "\t\tfor c := 0; c < 20; c++ {\n"
            + "\t\t\tfor i := 0; i < N; i++ {\n"
            + "\t\t\t\tm.Delete(i)\n"
            + "\t\t\t}\n"
            + "\t\t\tfor i := 0; i < N; i++ {\n"
            + "\t\t\t\tm.Store(i, i+1)\n"
            + "\t\t\t}\n"
            + "\t\t}\n"
            + "\t}()\n"
            + "\tbad := 0\n"
            + "\tfor r := 0; r < 5; r++ {\n"
            + "\t\tm.Range(func(k, v any) bool {\n"
            + "\t\t\tif v == nil {\n"
            + "\t\t\t\tbad++\n"
            + "\t\t\t}\n"
            + "\t\t\treturn true\n"
            + "\t\t})\n"
            + "\t}\n"
            + "\tfmt.Println(bad)\n"
            + "}\n";

    /** Repetitions of the smoke test: one is enough (see above). */
    private static final int CONCURRENT_REPS = 1;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-syncmap-range-test");
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
            code = P3SyncMain.run(new String[] {goFile.toString()});
        } finally {
            System.setOut(oldOut);
            System.setErr(oldErr);
        }
        return new RunResult(code, outBuf.toString(StandardCharsets.UTF_8),
                errBuf.toString(StandardCharsets.UTF_8));
    }

    private record RunResult(int exit, String stdout, String stderr) {}

    private static void checkOnce(String name, String prog, String expect) throws Exception {
        Path go = writeTempGo(name, prog);
        RunResult r = runProgram(go);
        assertTrue(r.exit() == 0,
                name + ": expected exit 0, got " + r.exit() + "; stderr: " + r.stderr());
        assertTrue(r.stdout().trim().equals(expect),
                name + ": expected stdout '" + expect + "', got <" + r.stdout().trim() + ">");
    }

    /** Range visits every stored entry, with the stored values. */
    public void testRangeVisitsAll() throws Exception {
        checkOnce("rangemapvisits.go", PROG_VISITS_ALL, "3 60");
    }

    /** Range skips keys deleted before the iteration starts. */
    public void testRangeSkipsDeleted() throws Exception {
        checkOnce("rangemapskip.go", PROG_SKIPS_DELETED, "3");
    }

    /** Range stops when the callback returns false. */
    public void testRangeEarlyStop() throws Exception {
        checkOnce("rangemapstop.go", PROG_EARLY_STOP, "2");
    }

    /**
     * R2316-01-P1-1 smoke test: concurrent Range+Delete must not crash,
     * hang, or deliver a nil value to the callback. The TOCTOU itself is
     * covered by {@code SyncOpNodeRangeTest} (see the class comment).
     */
    public void testRangeConcurrentDeleteNeverNil() throws Exception {
        Path go = writeTempGo("rangemapconc.go", PROG_CONCURRENT_DELETE);
        for (int i = 0; i < CONCURRENT_REPS; i++) {
            RunResult r = runProgram(go);
            assertTrue(r.exit() == 0,
                    "rangemapconc rep " + i + ": expected exit 0, got " + r.exit()
                            + "; stderr: " + r.stderr());
            assertTrue(r.stdout().trim().equals("0"),
                    "rangemapconc rep " + i + ": callback saw nil for "
                            + r.stdout().trim() + " absent key(s)");
        }
    }
}
