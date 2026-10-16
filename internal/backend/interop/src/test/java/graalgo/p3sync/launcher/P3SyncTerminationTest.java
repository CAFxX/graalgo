package graalgo.p3sync.launcher;

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

/**
 * P0-1 + P0-2 regression tests: scheduler termination semantics through
 * the real product launcher ({@code P3SyncMain}).
 *
 * <p>These are the review's verbatim regression tests (a)/(b)/(c), plus
 * the Goexit and pending-timer variants that pin down the exact
 * termination contract:
 * <ul>
 *   <li>(a) a {@code select {}} background goroutine is abandoned when
 *       main returns → exit 0 (P0-1);
 *   <li>(b) {@code main(){ c := make(chan int); <-c }} → gc's
 *       {@code fatal error: all goroutines are asleep - deadlock!},
 *       exit 2 (P0-2);
 *   <li>(c) main panics while a background goroutine is parked → the
 *       panic is reported (exit 2), not the deadlock;
 *   <li>main Goexits alone → gc's {@code fatal error: no goroutines
 *       (main called runtime.Goexit) - deadlock!}, exit 2;
 *   <li>main Goexits with a finished background → same "no goroutines"
 *       fatal, exit 2 (never exit 0);
 *   <li>main Goexits with a parked background → the all-asleep fatal,
 *       exit 2 (pre-fix: hang);
 *   <li>a pending {@code time.After(time.Hour)} is abandoned on main
 *       return → exit 0, no leaked timer translators.
 * </ul>
 *
 * <p>Each program runs in a SUBPROCESS (fresh JVM, cf. GoDebugTest):
 * abandoned goroutines (parked {@code select {}}s, pending timers) must
 * not leak into the test JVM, and a background goroutine admitted
 * after {@code P3SyncMain} uninstalled its scheduler would die with a
 * spurious "no SchedulerCore installed" internal error — a
 * test-harness race, not a product bug (a real process exits). The
 * subprocess timeout IS the red signal: on pre-fix code the hang
 * scenarios never return.
 *
 * <p>gc ground truth is documented on the SchedMain twin
 * ({@code graalgo.sched.launcher.SchedTerminationTest}); this class
 * asserts the product launcher honors the same contract.
 *
 * <p>verifies: P0-1, P0-2 (independent review 2026-10-16)
 */
public final class P3SyncTerminationTest {

    /** P0-1: background select{} must be abandoned when main returns. */
    private static final String PROG_BG_SELECT_OUTLIVES_MAIN =
            "package main\n\nfunc main() {\n\tgo func() { select {} }()\n}\n";

    /** P0-2: main parked on an empty channel: gc's fatal deadlock. */
    private static final String PROG_ALL_PARKED_DEADLOCK =
            "package main\n\nfunc main() {\n\tc := make(chan int)\n\t<-c\n}\n";

    /** Main panics while a background goroutine is parked: panic wins. */
    private static final String PROG_PANIC_WITH_PARKED_BG =
            "package main\n\nfunc main() {\n\tgo func() { select {} }()\n\tpanic(\"boom\")\n}\n";

    /** Main Goexits alone: gc's "no goroutines" fatal. */
    private static final String PROG_GOEXIT_ALONE =
            "package main\n\nimport \"runtime\"\n\nfunc main() { runtime.Goexit() }\n";

    /** Main Goexits; background finishes: still "no goroutines", exit 2. */
    private static final String PROG_GOEXIT_BG_FINISHES =
            "package main\n\nimport \"runtime\"\n\n"
            + "func main() {\n\tgo func() { println(\"bg done\") }()\n\truntime.Goexit()\n}\n";

    /** Main Goexits; background parks forever: the all-asleep fatal. */
    private static final String PROG_GOEXIT_BG_PARKED =
            "package main\n\nimport \"runtime\"\n\n"
            + "func main() {\n\tgo func() { select {} }()\n\truntime.Goexit()\n}\n";

    /**
     * P3-only: a pending {@code time.After(time.Hour)} must not delay
     * exit when main returns (gc abandons pending timers at process
     * exit), and its translator must be shut down, not leaked.
     */
    private static final String PROG_PENDING_TIMER_ABANDONED =
            "package main\n\nimport \"time\"\n\n"
            + "func main() {\n\t_ = time.After(time.Hour)\n\tprintln(\"main done\")\n}\n";

    private static final String DEADLOCK_LINE =
            "fatal error: all goroutines are asleep - deadlock!";
    private static final String NO_GOROUTINES_LINE =
            "fatal error: no goroutines (main called runtime.Goexit) - deadlock!";

    /**
     * Subprocess timeout: the pre-fix hang must fail, not stall the
     * suite.
     */
    private static final long SUBPROCESS_TIMEOUT_SECONDS = 90;

    /** P0-1 through the product launcher. Pre-fix: hang in the background drain. */
    public void testBackgroundSelectAbandonedOnMainReturn() throws Exception {
        ProcessResult r = runSubprocess("p3bgselect.go", PROG_BG_SELECT_OUTLIVES_MAIN);
        assertEquals(0, r.exitCode, "exit code, stderr:\n" + r.stderr);
    }

    /** P0-2 through the product launcher. Pre-fix: silent hang. */
    public void testAllParkedReportsFatalDeadlock() throws Exception {
        ProcessResult r = runSubprocess("p3deadlock.go", PROG_ALL_PARKED_DEADLOCK);
        assertEquals(2, r.exitCode, "exit code, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains(DEADLOCK_LINE),
                "stderr reports the gc deadlock line, got:\n" + r.stderr);
    }

    /** Panic wins over deadlock through the product launcher. Pre-fix: hang. */
    public void testMainPanicWithParkedBackgroundReportsPanic() throws Exception {
        ProcessResult r = runSubprocess("p3panicbg.go", PROG_PANIC_WITH_PARKED_BG);
        assertEquals(2, r.exitCode, "exit code, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains("panic: boom"),
                "stderr reports the panic, got:\n" + r.stderr);
        assertTrue(!r.stderr.contains(DEADLOCK_LINE),
                "no deadlock line when the panic is the report, got:\n" + r.stderr);
    }

    /** Goexit alone through the product launcher: "no goroutines", exit 2. */
    public void testGoexitAloneReportsNoGoroutines() throws Exception {
        ProcessResult r = runSubprocess("p3goexit.go", PROG_GOEXIT_ALONE);
        assertEquals(2, r.exitCode, "exit code, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains(NO_GOROUTINES_LINE),
                "stderr reports the gc no-goroutines line, got:\n" + r.stderr);
    }

    /** Goexit + finished background: "no goroutines", exit 2 (pre-fix: 0). */
    public void testGoexitWithFinishedBackgroundReportsNoGoroutines() throws Exception {
        ProcessResult r = runSubprocess("p3goexitbg.go", PROG_GOEXIT_BG_FINISHES);
        assertEquals(2, r.exitCode, "exit code, stderr:\n" + r.stderr);
        // Go's builtin println writes to stderr.
        assertTrue(r.stderr.contains("bg done"),
                "background goroutine ran to completion, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains(NO_GOROUTINES_LINE),
                "stderr reports the gc no-goroutines line, got:\n" + r.stderr);
    }

    /** Goexit + parked background: the all-asleep fatal, exit 2 (pre-fix: hang). */
    public void testGoexitWithParkedBackgroundReportsDeadlock() throws Exception {
        ProcessResult r = runSubprocess("p3goexitpark.go", PROG_GOEXIT_BG_PARKED);
        assertEquals(2, r.exitCode, "exit code, stderr:\n" + r.stderr);
        assertTrue(r.stderr.contains(DEADLOCK_LINE),
                "stderr reports the gc deadlock line, got:\n" + r.stderr);
    }

    /**
     * A pending hour-long timer must not delay exit, and its translator
     * must be shut down (not leaked into P3TimeRuntime's static set).
     */
    public void testPendingTimerAbandonedOnMainReturn() throws Exception {
        ProcessResult r = runSubprocess("p3timer.go", PROG_PENDING_TIMER_ABANDONED);
        assertEquals(0, r.exitCode, "exit code, stderr:\n" + r.stderr);
        // Go's builtin println writes to stderr.
        assertTrue(r.stderr.contains("main done"),
                "main ran to completion, stderr:\n" + r.stderr);
    }

    // ------------------------------------------------------------------
    // Harness: subprocess (fresh JVM per program)
    // ------------------------------------------------------------------

    private record ProcessResult(String stdout, String stderr, int exitCode) {}

    /**
     * Runs one program through a fresh {@code P3SyncMain} JVM. A timeout
     * destroys the subprocess and fails the test — on pre-fix code the
     * hang scenarios never return, so the timeout IS the red signal.
     */
    private static ProcessResult runSubprocess(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-p3sync-term-test");
        Path goFile = dir.resolve(name);
        Files.writeString(goFile, src, StandardCharsets.UTF_8);
        String javaBin = Path.of(System.getProperty("java.home"), "bin", "java").toString();
        List<String> cmd = new ArrayList<>();
        cmd.add(javaBin);
        cmd.add("--enable-native-access=ALL-UNNAMED");
        cmd.add("-Xss32m");
        cmd.add("-cp");
        cmd.add(System.getProperty("java.class.path"));
        cmd.add("graalgo.p3sync.launcher.P3SyncMain");
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
            if (line.startsWith("WARNING:")
                    || line.startsWith("OpenJDK 64-Bit Server VM warning:")) {
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
