package graalgo.sched.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * P0-1 + P0-2 regression tests: scheduler termination semantics through
 * the real {@code SchedMain} launcher.
 *
 * <p>Note on expressible constructs: SchedMain is the Phase-2 launcher —
 * its frontend has no channel/select/time surface (those are
 * P3SyncMain's), so the channel/select programs live in
 * {@code graalgo.p3sync.launcher.P3SyncTerminationTest}, verbatim from
 * the review. Here the same semantics are covered with constructs
 * SchedMain supports: a never-ending spinning background goroutine
 * stands in for a parked one (both never finish, so both hung the old
 * {@code awaitQuiescence}), and the deadlock watchdog itself is unit
 * tested at the scheduler level in {@code SchedulerCoreTest}.
 *
 * <p>The spinning-background tests run in a SUBPROCESS (fresh JVM per
 * program, cf. {@code GoDebugTest}): an abandoned infinite loop keeps
 * burning CPU in-JVM and starves later tests' subprocess launches —
 * flaky, not a product bug. The subprocess timeout IS the red signal:
 * on pre-fix code these programs hang forever.
 *
 * <p>gc ground truth (probed against go1.27.1):
 * <ul>
 *   <li>background goroutine outlives main → main's return exits 0,
 *       background abandoned (P0-1);
 *   <li>main panics while a background goroutine is stuck → the panic
 *       is reported, exit 2, no deadlock line;
 *   <li>main calls {@code runtime.Goexit}: the main goroutine dies, the
 *       program runs on, and it can never exit 0 afterwards — remaining
 *       goroutines finishing yields {@code fatal error: no goroutines
 *       (main called runtime.Goexit) - deadlock!}, exit 2.
 * </ul>
 *
 * <p>verifies: P0-1, P0-2 (independent review 2026-10-16)
 */
public final class SchedTerminationTest {

    /**
     * P0-1: a background goroutine that never finishes must be
     * abandoned when main returns (a spinning loop stands in for
     * {@code select {}} — SchedMain has no channel surface; both never
     * finish, so both hung the old awaitQuiescence). Subprocess: the
     * abandoned spinner must not leak into this JVM.
     */
    private static final String PROG_BG_SPIN_OUTLIVES_MAIN =
            "package main\n\nfunc main() {\n\tgo func() { for {} }()\n}\n";

    /** Main panics while a background goroutine is stuck: panic wins. */
    private static final String PROG_PANIC_WITH_STUCK_BG =
            "package main\n\nfunc main() {\n\tgo func() { for {} }()\n\tpanic(\"boom\")\n}\n";

    /** Main Goexits alone: gc's "no goroutines" fatal. */
    private static final String PROG_GOEXIT_ALONE =
            "package main\n\nimport \"runtime\"\n\nfunc main() { runtime.Goexit() }\n";

    /** Main Goexits; background finishes: still "no goroutines", exit 2. */
    private static final String PROG_GOEXIT_BG_FINISHES =
            "package main\n\nimport \"runtime\"\n\n"
            + "func main() {\n\tgo func() { println(\"bg done\") }()\n\truntime.Goexit()\n}\n";

    private static final String DEADLOCK_LINE =
            "fatal error: all goroutines are asleep - deadlock!";
    private static final String NO_GOROUTINES_LINE =
            "fatal error: no goroutines (main called runtime.Goexit) - deadlock!";

    /** In-JVM hard timeout per program. */
    private static final long TIMEOUT_SECONDS = 60;
    /** Subprocess timeout: the pre-fix hang must fail, not stall the suite. */
    private static final long SUBPROCESS_TIMEOUT_SECONDS = 90;

    // ------------------------------------------------------------------
    // P0-1 (subprocess: abandoned spinners must not leak into this JVM)
    // ------------------------------------------------------------------

    /**
     * P0-1: {@code go func() { for{} }()} + main returns → exit 0.
     * Pre-fix: awaitQuiescence waited for the stuck goroutine forever
     * (subprocess timeout).
     */
    public void testBackgroundSpinAbandonedOnMainReturn() throws Exception {
        ProcessResult r = runSubprocess("bgspin.go", PROG_BG_SPIN_OUTLIVES_MAIN);
        assertEquals(0, r.exitCode, "exit code, stderr:\n" + r.stderr);
    }

    /**
     * Main panics while a background goroutine is stuck → the panic is
     * reported (exit 2), not the deadlock.
     * Pre-fix: the panic was recorded but awaitQuiescence hung on the
     * stuck background goroutine (subprocess timeout).
     */
    public void testMainPanicWithStuckBackgroundReportsPanic() throws Exception {
        ProcessResult r = runSubprocess("panicbg.go", PROG_PANIC_WITH_STUCK_BG);
        assertEquals(2, r.exitCode, "exit code, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains("panic: boom"),
                "stderr reports the panic, got:\n" + r.stderr);
        assertTrue(!r.stderr.contains(DEADLOCK_LINE),
                "no deadlock line when the panic is the report, got:\n" + r.stderr);
    }

    // ------------------------------------------------------------------
    // Goexit termination (in-JVM: no leaks; same root cause family)
    // ------------------------------------------------------------------

    /**
     * {@code main(){ runtime.Goexit() }} alone → gc's "no goroutines"
     * fatal, exit 2. Pre-fix: printed the wrong ("all goroutines are
     * asleep") line.
     */
    public void testGoexitAloneReportsNoGoroutines() throws Exception {
        RunResult r = runProgram("goexit.go", PROG_GOEXIT_ALONE);
        assertEquals(2, r.exit, "exit code, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains(NO_GOROUTINES_LINE),
                "stderr reports the gc no-goroutines line, got:\n" + r.stderr);
    }

    /**
     * Main Goexits; the background goroutine runs to completion → the
     * program still cannot exit 0: gc's "no goroutines" fatal, exit 2.
     * Pre-fix: exit 0. (Go's builtin println writes to stderr.)
     */
    public void testGoexitWithFinishedBackgroundReportsNoGoroutines() throws Exception {
        RunResult r = runProgram("goexitbg.go", PROG_GOEXIT_BG_FINISHES);
        assertEquals(2, r.exit, "exit code, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains("bg done"),
                "background goroutine ran to completion, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains(NO_GOROUTINES_LINE),
                "stderr reports the gc no-goroutines line, got:\n" + r.stderr);
    }

    // ------------------------------------------------------------------
    // Harness: in-JVM (no leaks)
    // ------------------------------------------------------------------

    private record RunResult(int exit, String stdout, String stderr) {}

    /**
     * Runs one program through {@link SchedMain#run} in-JVM, capturing
     * stdout/stderr, with a hard timeout. Only for leak-free programs;
     * spinning backgrounds use {@link #runSubprocess}.
     */
    private static RunResult runProgram(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-sched-term-test");
        Path goFile = dir.resolve(name);
        Files.writeString(goFile, src, StandardCharsets.UTF_8);
        var exec = Executors.newSingleThreadExecutor();
        try {
            Future<RunResult> f = exec.submit(() -> {
                PrintStream oldOut = System.out;
                PrintStream oldErr = System.err;
                ByteArrayOutputStream outBuf = new ByteArrayOutputStream();
                ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
                System.setOut(new PrintStream(outBuf, true, StandardCharsets.UTF_8));
                System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
                int code;
                try {
                    code = SchedMain.run(new String[] {"--gmp=1", goFile.toString()});
                } finally {
                    System.setOut(oldOut);
                    System.setErr(oldErr);
                }
                return new RunResult(code, outBuf.toString(StandardCharsets.UTF_8),
                        errBuf.toString(StandardCharsets.UTF_8));
            });
            try {
                return f.get(TIMEOUT_SECONDS, TimeUnit.SECONDS);
            } catch (TimeoutException te) {
                f.cancel(true);
                throw new AssertionError(
                        name + ": launcher did not terminate within " + TIMEOUT_SECONDS
                        + "s (hang — P0-1/P0-2 not fixed)");
            }
        } finally {
            exec.shutdownNow();
        }
    }

    // ------------------------------------------------------------------
    // Harness: subprocess (fresh JVM; abandoned spinners die with it)
    // ------------------------------------------------------------------

    private record ProcessResult(String stdout, String stderr, int exitCode) {}

    /**
     * Runs one program through a fresh {@code SchedMain} JVM. A timeout
     * destroys the subprocess and fails the test — on pre-fix code the
     * hang scenarios never return, so the timeout IS the red signal.
     */
    private static ProcessResult runSubprocess(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-sched-term-test");
        Path goFile = dir.resolve(name);
        Files.writeString(goFile, src, StandardCharsets.UTF_8);
        String javaBin = Path.of(System.getProperty("java.home"), "bin", "java").toString();
        List<String> cmd = new ArrayList<>();
        cmd.add(javaBin);
        cmd.add("--enable-native-access=ALL-UNNAMED");
        cmd.add("-Xss32m");
        cmd.add("-cp");
        cmd.add(System.getProperty("java.class.path"));
        cmd.add("graalgo.sched.launcher.SchedMain");
        cmd.add("--gmp=1");
        cmd.add(goFile.toString());
        ProcessBuilder pb = new ProcessBuilder(cmd);
        // Inherit environment (notably GRAALGO_GOFRONTEND for the build).
        Process p = pb.start();
        Future<String> outF = readAsync(p.getInputStream());
        Future<String> errF = readAsync(p.getErrorStream());
        boolean done = p.waitFor(SUBPROCESS_TIMEOUT_SECONDS, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(name + ": subprocess did not terminate within "
                    + SUBPROCESS_TIMEOUT_SECONDS + "s (hang — P0-1/P0-2 not fixed)");
        }
        String stdout;
        String stderr;
        try {
            stdout = outF.get(10, TimeUnit.SECONDS);
            stderr = errF.get(10, TimeUnit.SECONDS);
        } catch (Exception e) {
            throw new AssertionError(name + ": failed to read subprocess streams", e);
        }
        return new ProcessResult(stripJvmWarnings(stdout), stripJvmWarnings(stderr),
                p.exitValue());
    }

    private static Future<String> readAsync(java.io.InputStream in) {
        var ex = Executors.newSingleThreadExecutor();
        Future<String> f = ex.submit(() -> {
            try (var stream = in) {
                return new String(stream.readAllBytes(), StandardCharsets.UTF_8);
            }
        });
        ex.shutdown();
        return f;
    }

    private static String stripJvmWarnings(String s) {
        StringBuilder sb = new StringBuilder();
        for (String line : s.split("\n", -1)) {
            if (line.startsWith("WARNING:") || line.startsWith("OpenJDK 64-Bit Server VM warning:")) {
                continue;
            }
            if (sb.length() > 0) {
                sb.append('\n');
            }
            sb.append(line);
        }
        return sb.toString();
    }

    private static void assertEquals(long expected, long actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }
}
