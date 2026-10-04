package graalgo.launcher;



import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.regex.Pattern;

/**
 * M7: program-level differential vs stock gc for corpus programs.
 *
 * <p>Red-green-first (SPEC §5.2): the expected values come from stock gc at
 * test time — the oracle is a freshly built gc binary, so the test cannot
 * pass by construction. Compares exit code and stdout exactly, and stderr
 * under the diffharness' documented normalization rules (addresses,
 * goroutine numbers, file paths/line numbers, and pc offsets are not
 * significant — see tools/diffharness/README.md "What is compared").
 *
 * <p>Both backends run with a scrubbed environment ({@code GOMAXPROCS=1},
 * {@code GOTOOLCHAIN=local}) in a fresh empty working directory, per the
 * harness nondeterminism policy.
 */
public final class Differential {
    private Differential() {}

    private static final Pattern ADDR = Pattern.compile("\\b0x[0-9a-fA-F]+\\b");
    private static final Pattern GOROUTINE = Pattern.compile("goroutine \\d+ \\[");
    private static final Pattern LOCATION =
            Pattern.compile("([/\\w.\\-]+/)?([\\w.\\-]+\\.go):\\d+");
    private static final Pattern PCOFF = Pattern.compile(" \\+0x[0-9a-fA-F]+\\b");

    /**
     * Normalizes stderr with the diffharness' documented equivalence rules.
     * Deliberately NOT normalized (a difference here is a real divergence):
     * message text, exit codes, timeouts, build failures.
     */
    static String normalizeStderr(String s) {
        // M7: PCOFF must run before ADDR — otherwise +0x3d becomes +0xADDR
        // and the pc-offset pattern (which expects hex) won't match.
        s = PCOFF.matcher(s).replaceAll("");
        s = ADDR.matcher(s).replaceAll("0xADDR");
        s = GOROUTINE.matcher(s).replaceAll("goroutine # [");
        s = LOCATION.matcher(s).replaceAll("$2:LINE");
        return s;
    }

    private record ProcResult(int exit, String stdout, String stderr) {}

    private static ProcResult runProc(ProcessBuilder pb, Path src, String what) throws Exception {
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.environment().put("GOMAXPROCS", "1");
        pb.directory(Files.createTempDirectory("graalgo-difftest-run").toFile());
        pb.redirectErrorStream(false);
        Process p = pb.start();
        String stdout = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        String stderr = new String(p.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
        if (!p.waitFor(120, TimeUnit.SECONDS)) {
            p.destroyForcibly();
            throw new AssertionError(src + ": " + what + " timed out");
        }
        return new ProcResult(p.exitValue(), stdout, stderr);
    }

    /**
     * Runs one corpus program under stock gc (freshly built binary) and
     * under graalgo (in-process), and asserts observable parity: exit code,
     * stdout byte-identical, stderr identical after normalization.
     */
    public static void checkProgram(Path src) throws Exception {
        src = src.toAbsolutePath().normalize();
        // Oracle: stock gc build + run of the binary (not `go run`, whose
        // own "exit status N" line would pollute stderr).
        Path dir = Files.createTempDirectory("graalgo-difftest");
        Path bin = dir.resolve("prog");
        ProcResult build = runProc(new ProcessBuilder(
                ToolPaths.stockGo().toString(), "build", "-o", bin.toString(), src.toString()),
                src, "gc build");
        if (build.exit != 0) {
            throw new AssertionError(src + ": stock gc build failed (exit " + build.exit + "): "
                    + build.stderr);
        }
        ProcResult expected = runProc(new ProcessBuilder(bin.toString()), src, "gc run");

        // Subject: in-process graalgo-run with captured streams. (The JVM's
        // own Unsafe warnings bypass System.err — they go to the OS stderr
        // directly — so the capture stays clean.)
        ByteArrayOutputStream outBuf = new ByteArrayOutputStream();
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        PrintStream oldOut = System.out;
        PrintStream oldErr = System.err;
        int actualCode;
        try {
            System.setOut(new PrintStream(outBuf, true, StandardCharsets.UTF_8));
            System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
            actualCode = Main.run(new String[] {src.toString()});
        } finally {
            System.setOut(oldOut);
            System.setErr(oldErr);
        }
        String actualOut = outBuf.toString(StandardCharsets.UTF_8);
        String actualErr = errBuf.toString(StandardCharsets.UTF_8);

        if (expected.exit != actualCode) {
            throw new AssertionError(src + ": exit code gc=" + expected.exit
                    + " graalgo=" + actualCode
                    + "\n--- graalgo stdout ---\n" + actualOut
                    + "\n--- graalgo stderr ---\n" + actualErr);
        }
        if (!expected.stdout.equals(actualOut)) {
            throw new AssertionError(src + ": stdout differs"
                    + "\n--- gc ---\n" + expected.stdout
                    + "\n--- graalgo ---\n" + actualOut);
        }
        String eErr = normalizeStderr(expected.stderr);
        String aErr = normalizeStderr(actualErr);
        if (!eErr.equals(aErr)) {
            throw new AssertionError(src + ": stderr differs (normalized)"
                    + "\n--- gc ---\n" + eErr
                    + "\n--- graalgo ---\n" + aErr);
        }
    }
}
