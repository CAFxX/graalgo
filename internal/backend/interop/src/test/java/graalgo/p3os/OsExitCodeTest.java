package graalgo.p3os;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * End-to-end process exit-code tests (P1: os.Exit lost the exit code).
 *
 * <p>{@code GoOs.exit} calls {@code System.exit}, which would kill the
 * in-JVM test runner — so these tests spawn the product launcher
 * ({@code graalgo-sync-run}) as a subprocess on small Go programs and
 * assert the host process exit status. Covers:
 * <ul>
 *   <li>{@code os.Exit(42)} → host exit 42 (the P1 regression).</li>
 *   <li>{@code os.Exit(0)} → host exit 0.</li>
 *   <li>Deferred functions do not run on {@code os.Exit} (gc semantics):
 *       the deferred println must not appear, and code after
 *       {@code os.Exit} must not execute.</li>
 * </ul>
 *
 * <p>Each test is demonstrably capable of failing: if the exit code were
 * dropped (exit 1) or the eface mechanism regressed (import crash → exit
 * 1), the assertions fail.
 */
public final class OsExitCodeTest {
    private static Path scratch;
    private static Path launcher;

    private static void init() throws IOException {
        if (scratch != null) {
            return;
        }
        // Hermetic scratch: the sanctioned $TMPDIR (never /tmp), falling
        // back to the module's gitignored out/test-tmp (never the repo
        // tree root — untracked debris breaks the pristine-tree gates).
        String tmpdir = System.getenv("TMPDIR");
        Path base = tmpdir != null
                ? Path.of(tmpdir, "junit")
                : Path.of(System.getProperty("user.dir"), "out", "test-tmp", "junit");
        Files.createDirectories(base);
        scratch = Files.createTempDirectory(base, "osexit-");
        launcher = findLauncher();
    }

    /** Locates internal/backend/graalgo-sync-run from the working directory. */
    private static Path findLauncher() {
        Path dir = Path.of(System.getProperty("user.dir")).toAbsolutePath();
        for (int i = 0; i < 8 && dir != null; i++) {
            Path cand = dir.resolve("internal/backend/graalgo-sync-run");
            if (Files.isExecutable(cand)) {
                return cand;
            }
            // Also accept being run from internal/backend itself.
            Path direct = dir.resolve("graalgo-sync-run");
            if (Files.isExecutable(direct)) {
                return direct;
            }
            dir = dir.getParent();
        }
        throw new IllegalStateException(
                "cannot locate graalgo-sync-run from " + System.getProperty("user.dir"));
    }

    /** Runs the launcher on a Go source file; returns (exitCode, combinedOutput). */
    private static Result runGo(Path src) throws IOException, InterruptedException {
        List<String> cmd = new ArrayList<>();
        cmd.add(launcher.toString());
        cmd.add(src.toString());
        ProcessBuilder pb = new ProcessBuilder(cmd);
        pb.directory(scratch.toFile());
        // Keep the child JVM off /tmp too: always propagate a TMPDIR (the
        // scratch base), since the launcher maps it to -Djava.io.tmpdir.
        pb.environment().put("TMPDIR", scratch.getParent().toString());
        pb.environment().put("GRAALGO_GOFRONTEND",
                launcher.getParent().resolve("../frontend/gofrontend").normalize().toString());
        pb.redirectErrorStream(true);
        Process p = pb.start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        int code = p.waitFor();
        return new Result(code, out);
    }

    private record Result(int exitCode, String output) {}

    private static Path writeGo(String name, String src) throws IOException {
        init();
        Path f = scratch.resolve(name);
        Files.writeString(f, src, StandardCharsets.UTF_8);
        return f;
    }

    private static void assertEquals(int want, int got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + ", got " + got);
        }
    }

    private static void assertContains(String haystack, String needle, String what) {
        if (!haystack.contains(needle)) {
            throw new AssertionError(what + ": expected output to contain <"
                    + needle + ">, got <" + haystack + ">");
        }
    }

    private static void assertNotContains(String haystack, String needle, String what) {
        if (haystack.contains(needle)) {
            throw new AssertionError(what + ": expected output NOT to contain <"
                    + needle + ">, got <" + haystack + ">");
        }
    }

    public void testExit42() throws IOException, InterruptedException {
        Path src = writeGo("exit42.go",
                "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(42) }\n");
        Result r = runGo(src);
        assertEquals(42, r.exitCode(), "os.Exit(42) host exit code");
    }

    public void testExit0() throws IOException, InterruptedException {
        Path src = writeGo("exit0.go",
                "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(0) }\n");
        Result r = runGo(src);
        assertEquals(0, r.exitCode(), "os.Exit(0) host exit code");
    }

    public void testDefersDoNotRun() throws IOException, InterruptedException {
        Path src = writeGo("exitdefer.go",
                "package main\n\nimport \"os\"\n\n"
                        + "func main() {\n"
                        + "\tdefer println(\"DEFERRED-MUST-NOT-PRINT\")\n"
                        + "\tprintln(\"before-exit\")\n"
                        + "\tos.Exit(7)\n"
                        + "\tprintln(\"after-exit-MUST-NOT-PRINT\")\n"
                        + "}\n");
        Result r = runGo(src);
        assertEquals(7, r.exitCode(), "os.Exit(7) host exit code");
        assertContains(r.output(), "before-exit", "defer test");
        assertNotContains(r.output(), "DEFERRED-MUST-NOT-PRINT", "defer test");
        assertNotContains(r.output(), "after-exit-MUST-NOT-PRINT", "defer test");
    }
}
