package graalgo.launcher;

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
 * R-01: the Phase-2 overlay check in {@link Main}.
 *
 * <p>The same-FQN shadow overlay (8 classes exist twice: the truffle
 * module's Phase-1 stubs and the runtime module's replacements) is only
 * correct when runtime/out/classes precedes truffle/out/classes on the
 * classpath. {@link Main} is the entry point {@code graalgo-run} actually
 * execs, so it must fail fast on a wrong order — the {@code --check}
 * diffharness build step included, which is why the assertion sits at
 * the top of {@link Main#run} rather than only before guest execution.
 *
 * <p>Falsifiability: {@link #testReversedClasspathFailsFast} builds a
 * stub {@code graalgo.runtime.Scheduler} with no {@code isPhase2}
 * method (mirroring the real truffle stub's shape: its
 * {@code spawn} throws {@code UnsupportedOperationException}), puts it
 * FIRST on a child JVM's classpath, and requires the child to die
 * with the FATAL overlay message instead of printing usage and
 * exiting 0. Before the R-01 fix that child exited 0; the test would
 * have failed. The stub is compiled at test time with the system
 * JavaCompiler so the test is hermetic: it does not depend on
 * truffle/out/classes having been built.
 *
 * <p>Review 2026-11-04 P1-10: the R-01 guard checked only Scheduler, so
 * a present-but-partial runtime build passed it while another class
 * silently fell back to its Phase-1 stub. {@link
 * #testPartialGoroutineStateFailsFast} and {@link
 * #testPartialSafepointFailsFast} close that hole: each puts a stub
 * for ONE other overlay class first — the Scheduler overlay itself
 * loads fine, so the old guard would have passed — and requires the
 * child to die with the FATAL message naming that class.
 */
public final class LauncherOverlayTest {

    /** verifies: R-01 (overlay check passes on the correct classpath). */
    public void testOverlayCheckPasses() {
        Main.assertPhase2Overlay();
    }

    /** verifies: R-01 (a normal launch path still succeeds on the correct classpath). */
    public void testHelpPassesOnCorrectClasspath() {
        assertEquals(Main.EXIT_OK, Main.run(new String[] {"--help"}), "--help exit code");
    }

    /**
     * verifies: R-01 (a stub Scheduler loaded first fails fast, never silently).
     */
    public void testReversedClasspathFailsFast() throws Exception {
        Path stubDir = null;
        try {
            stubDir = compileStubScheduler();
            runChildMainExpectFatal(stubDir, "Phase-1 Scheduler stub");
        } finally {
            if (stubDir != null) {
                deleteTree(stubDir.getParent()); // the temp root (stub-classes + leftovers)
            }
        }
    }

    /**
     * verifies: review 2026-11-04 P1-10 (a partial classpath fails fast).
     * Only GoroutineState is stubbed: the Scheduler overlay loads fine,
     * so the old Scheduler-only guard would have passed and the run
     * would have silently shared one static holder across goroutines.
     * The new all-8-classes guard must name GoroutineState and die.
     */
    public void testPartialGoroutineStateFailsFast() throws Exception {
        Path stubDir = null;
        try {
            stubDir = compileStubClass("graalgo.runtime", "GoroutineState",
                    "package graalgo.runtime;\n"
                    + "public final class GoroutineState {\n"
                    + "    private GoroutineState() {}\n"
                    + "    public static GoroutineState current() {\n"
                    + "        return null;\n"
                    + "    }\n"
                    + "}\n");
            runChildMainExpectFatal(stubDir, "Phase-1 GoroutineState stub");
        } finally {
            if (stubDir != null) {
                deleteTree(stubDir.getParent());
            }
        }
    }

    /**
     * verifies: review 2026-11-04 P1-10 (a partial classpath fails fast).
     * Only Safepoint is stubbed: the Scheduler overlay loads fine, so
     * the old Scheduler-only guard would have passed and the run
     * would have silently skipped every quantum check (the Phase-1
     * no-op). The new guard must name Safepoint and die.
     */
    public void testPartialSafepointFailsFast() throws Exception {
        Path stubDir = null;
        try {
            stubDir = compileStubClass("graalgo.safepoint", "Safepoint",
                    "package graalgo.safepoint;\n"
                    + "public final class Safepoint {\n"
                    + "    private Safepoint() {}\n"
                    + "    public static void poll() {\n"
                    + "    }\n"
                    + "}\n");
            runChildMainExpectFatal(stubDir, "Phase-1 Safepoint stub");
        } finally {
            if (stubDir != null) {
                deleteTree(stubDir.getParent());
            }
        }
    }

    /**
     * Runs {@code graalgo.launcher.Main --help} in a child JVM with
     * {@code stubDir} FIRST on the classpath (ahead of the real runtime
     * classes) and requires it to die with the FATAL overlay message
     * naming {@code expectedSnippet}. Main's guard runs before help
     * handling, so a passing guard would print usage and exit 0.
     */
    private static void runChildMainExpectFatal(Path stubDir, String expectedSnippet)
            throws Exception {
        Path runtimeClasses = runtimeClassesDir();
        List<String> cmd = new ArrayList<>();
        cmd.add(Paths.get(System.getProperty("java.home"), "bin", "java").toString());
        StringBuilder cp = new StringBuilder();
        cp.append(stubDir).append(java.io.File.pathSeparator).append(runtimeClasses);
        for (String entry : System.getProperty("java.class.path").split(java.io.File.pathSeparator)) {
            if (entry.endsWith(".jar")) {
                cp.append(java.io.File.pathSeparator).append(entry);
            }
        }
        cmd.add("-cp");
        cmd.add(cp.toString());
        cmd.add("graalgo.launcher.Main");
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
            throw new AssertionError("child JVM did not exit within 120s (stub "
                    + expectedSnippet + " first)");
        }
        assertTrue(p.exitValue() != 0,
                "child with stub " + expectedSnippet + " first must NOT exit 0; output was: " + out);
        assertTrue(out.contains("FATAL") && out.contains(expectedSnippet),
                "child must report the FATAL overlay message naming " + expectedSnippet
                + "; output was: " + out);
    }

    /** Directory holding the compiled {@code graalgo.launcher.Main} (runtime/out/classes). */
    private static Path runtimeClassesDir() throws Exception {
        java.net.URL loc = Main.class.getProtectionDomain().getCodeSource().getLocation();
        assertTrue(loc != null, "Main code source must be available");
        Path dir = Path.of(loc.toURI());
        assertTrue(Files.isDirectory(dir),
                "Main must run from a classes directory, not a jar: " + dir);
        assertTrue(Files.isRegularFile(dir.resolve("graalgo/launcher/Main.class")),
                "Main.class must be under the runtime classes dir: " + dir);
        return dir;
    }

    /** Compiles a stub Scheduler (no isPhase2) into a fresh temp directory; returns the dir. */
    private static Path compileStubScheduler() throws Exception {
        return compileStubClass("graalgo.runtime", "Scheduler",
                "package graalgo.runtime;\n"
                + "import graalgo.values.GoFunc;\n"
                + "public final class Scheduler {\n"
                + "    private Scheduler() {}\n"
                + "    public static void spawn(GoFunc fn) {\n"
                + "        throw new UnsupportedOperationException(\n"
                + "                \"Phase 2: goroutine creation needs the scheduler (GG-0020)\");\n"
                + "    }\n"
                + "}\n");
    }

    /**
     * Compiles one stub class (no {@code isPhase2} method, mirroring the
     * real truffle stub's shape for that class) into a fresh temp
     * directory and returns the classes dir to put first on a child
     * JVM's classpath. The stub source is compiled at test time with
     * the system JavaCompiler so the test is hermetic: it does not
     * depend on truffle/out/classes having been built.
     */
    private static Path compileStubClass(String packageName, String simpleName, String source)
            throws Exception {
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        assertTrue(compiler != null,
                "system JavaCompiler must be available (pinned GraalVM JDK, not a JRE)");
        // Self-sufficient temp parent: createTempDirectory does not create a
        // missing parent, and the Makefile's mkdir of out/test-tmp must not
        // be a precondition for running this test directly.
        Path tmpParent = Path.of(System.getProperty("java.io.tmpdir"));
        Files.createDirectories(tmpParent);
        Path dir = Files.createTempDirectory(tmpParent, "graalgo-overlay-test-");
        String packagePath = packageName.replace('.', '/');
        Path srcDir = dir.resolve("src/" + packagePath);
        Files.createDirectories(srcDir);
        Path src = srcDir.resolve(simpleName + ".java");
        Files.writeString(src, source, StandardCharsets.UTF_8);
        Path outDir = dir.resolve("classes");
        Files.createDirectories(outDir);
        int rc = compiler.run(null, null, null,
                "-cp", runtimeClassesDir().toString(),
                "-d", outDir.toString(), src.toString());
        assertTrue(rc == 0, "stub " + simpleName + " must compile (javac exit " + rc + ")");
        // Flatten: child classpath entry is the classes dir itself.
        Path flat = dir.resolve("stub-classes");
        Files.createDirectories(flat);
        Path stubClass = flat.resolve(packagePath + "/" + simpleName + ".class");
        Files.createDirectories(stubClass.getParent());
        Files.move(outDir.resolve(packagePath + "/" + simpleName + ".class"), stubClass);
        assertTrue(Files.isRegularFile(stubClass),
                "stub " + simpleName + ".class must exist, else the test is vacuous: " + stubClass);
        deleteTree(dir.resolve("src"));
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
