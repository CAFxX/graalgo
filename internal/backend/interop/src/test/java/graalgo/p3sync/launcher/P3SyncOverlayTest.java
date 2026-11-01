package graalgo.p3sync.launcher;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.FileVisitResult;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.SimpleFileVisitor;
import java.nio.file.attribute.BasicFileAttributes;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import javax.tools.JavaCompiler;
import javax.tools.ToolProvider;

/**
 * P1-5: the product launcher's Phase-2 overlay check.
 *
 * <p>Mirrors {@code graalgo.launcher.LauncherOverlayTest} (R-01) for
 * {@code P3SyncMain}. The P1-5 gap was specific to this launcher: its
 * assert used to sit only before guest execution, so the
 * {@code --check} build step — the diffharness build step for the
 * product launcher — exited 0 with the Phase-1 stub loaded. The
 * {@code --help} probe below is the hermetic representative of that
 * whole early-return class: before the fix, {@code --help} also
 * returned before reaching the assert and exited 0 on a reversed
 * classpath; after the fix the assert at the top of {@link P3SyncMain#run}
 * throws FATAL before any argument handling.
 *
 * <p>Falsifiability: the reversed test builds a stub
 * {@code graalgo.runtime.Scheduler} with no {@code isPhase2} method
 * (mirroring the real truffle stub's shape), puts it FIRST on a child
 * JVM's classpath, and requires the child to die with the FATAL
 * overlay message instead of printing usage and exiting 0.
 */
public final class P3SyncOverlayTest {

    /** verifies: P1-5 (overlay check passes on the correct classpath). */
    public void testOverlayCheckPasses() {
        graalgo.launcher.OverlayGate.assertSchedulerOverlay();
    }

    /** verifies: P1-5 (a normal launch path still succeeds on the correct classpath). */
    public void testHelpPassesOnCorrectClasspath() {
        assertEquals(P3SyncMain.EXIT_OK, P3SyncMain.run(new String[] {"--help"}),
                "--help exit code");
    }

    /**
     * verifies: P1-5 (a stub Scheduler loaded first fails fast on the
     * --help early-return path, never silently).
     */
    public void testReversedClasspathFailsFast() throws Exception {
        Path stubDir = null;
        try {
            stubDir = compileStubScheduler();
            List<String> cmd = new ArrayList<>();
            cmd.add(Paths.get(System.getProperty("java.home"), "bin", "java").toString());
            StringBuilder cp = new StringBuilder();
            cp.append(stubDir).append(java.io.File.pathSeparator)
                    .append(interopClassesDir()).append(java.io.File.pathSeparator)
                    .append(runtimeClassesDir());
            for (String entry : System.getProperty("java.class.path").split(java.io.File.pathSeparator)) {
                if (entry.endsWith(".jar")) {
                    cp.append(java.io.File.pathSeparator).append(entry);
                }
            }
            cmd.add("-cp");
            cmd.add(cp.toString());
            cmd.add("graalgo.p3sync.launcher.P3SyncMain");
            cmd.add("--help");
            Process p = new ProcessBuilder(cmd).redirectErrorStream(true).start();
            String out;
            try {
                out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            } catch (IOException e) {
                p.destroyForcibly();
                throw e;
            }
            boolean exited = p.waitFor(120, TimeUnit.SECONDS);
            if (!exited) {
                p.destroyForcibly();
                throw new AssertionError("child JVM did not exit within 120s (reversed classpath)");
            }
            assertTrue(p.exitValue() != 0,
                    "child with stub Scheduler first must NOT exit 0; output was: " + out);
            assertTrue(out.contains("FATAL") && out.contains("Phase-1 Scheduler stub"),
                    "child must report the FATAL overlay message; output was: " + out);
        } finally {
            if (stubDir != null) {
                deleteTree(stubDir.getParent()); // the temp root (stub-classes + leftovers)
            }
        }
    }

    /** Directory holding the compiled {@code graalgo.p3sync.launcher.P3SyncMain} (interop/out/classes). */
    private static Path interopClassesDir() throws Exception {
        java.net.URL loc = P3SyncMain.class.getProtectionDomain().getCodeSource().getLocation();
        assertTrue(loc != null, "P3SyncMain code source must be available");
        Path dir = Path.of(loc.toURI());
        assertTrue(Files.isDirectory(dir),
                "P3SyncMain must run from a classes directory, not a jar: " + dir);
        assertTrue(Files.isRegularFile(dir.resolve("graalgo/p3sync/launcher/P3SyncMain.class")),
                "P3SyncMain.class must be under the interop classes dir: " + dir);
        return dir;
    }

    /** Directory holding the compiled {@code graalgo.runtime.Scheduler} replacement (runtime/out/classes). */
    private static Path runtimeClassesDir() throws Exception {
        java.net.URL loc = graalgo.runtime.Scheduler.class.getProtectionDomain().getCodeSource().getLocation();
        assertTrue(loc != null, "Scheduler code source must be available");
        Path dir = Path.of(loc.toURI());
        assertTrue(Files.isDirectory(dir),
                "Scheduler must run from a classes directory, not a jar: " + dir);
        assertTrue(Files.isRegularFile(dir.resolve("graalgo/runtime/Scheduler.class")),
                "Scheduler.class must be under the runtime classes dir: " + dir);
        return dir;
    }

    /** Compiles a stub Scheduler (no isPhase2) into a fresh temp directory; returns the dir. */
    private static Path compileStubScheduler() throws Exception {
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        assertTrue(compiler != null,
                "system JavaCompiler must be available (pinned GraalVM JDK, not a JRE)");
        Path tmpParent = Path.of(System.getProperty("java.io.tmpdir"));
        Files.createDirectories(tmpParent);
        Path dir = Files.createTempDirectory(tmpParent, "graalgo-p3sync-overlay-test-");
        Path srcDir = dir.resolve("src/graalgo/runtime");
        Files.createDirectories(srcDir);
        Path src = srcDir.resolve("Scheduler.java");
        Files.writeString(src,
                "package graalgo.runtime;\n"
                + "import graalgo.values.GoFunc;\n"
                + "public final class Scheduler {\n"
                + "    private Scheduler() {}\n"
                + "    public static void spawn(GoFunc fn) {\n"
                + "        throw new UnsupportedOperationException(\n"
                + "                \"Phase 2: goroutine creation needs the scheduler (GG-0020)\");\n"
                + "    }\n"
                + "}\n",
                StandardCharsets.UTF_8);
        Path outDir = dir.resolve("classes");
        Files.createDirectories(outDir);
        int rc = compiler.run(null, null, null,
                "-cp", runtimeClassesDir().toString(),
                "-d", outDir.toString(), src.toString());
        assertTrue(rc == 0, "stub Scheduler must compile (javac exit " + rc + ")");
        // Flatten: child classpath entry is the classes dir itself.
        Path flat = dir.resolve("stub-classes");
        Files.createDirectories(flat);
        Files.createDirectories(flat.resolve("graalgo/runtime"));
        Files.move(outDir.resolve("graalgo/runtime/Scheduler.class"),
                flat.resolve("graalgo/runtime/Scheduler.class"));
        deleteTree(srcDir.getParent().getParent()); // src/ tree (dir/src)
        deleteTree(outDir);
        return flat;
    }

    private static void deleteTree(Path root) throws IOException {
        if (!Files.exists(root)) {
            return;
        }
        Files.walkFileTree(root, new SimpleFileVisitor<>() {
            @Override
            public FileVisitResult visitFile(Path file, BasicFileAttributes attrs) throws IOException {
                Files.deleteIfExists(file);
                return FileVisitResult.CONTINUE;
            }

            @Override
            public FileVisitResult postVisitDirectory(Path dir, IOException exc) throws IOException {
                Files.deleteIfExists(dir);
                return FileVisitResult.CONTINUE;
            }
        });
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(int expected, int actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }
}
