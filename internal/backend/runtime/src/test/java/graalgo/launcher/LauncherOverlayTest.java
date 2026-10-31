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
 * <p>R-02 extended the check from Scheduler alone to all eight
 * shadows: each class now carries its own {@code isPhase2()} marker,
 * and there is one reversed-classpath test per class below, so a
 * regression names the class that loaded wrong.
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
 */
public final class LauncherOverlayTest {

    /** verifies: R-01/R-02 (overlay check passes on the correct classpath). */
    public void testOverlayCheckPasses() {
        Main.assertOverlay();
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
            Path stubClass = stubDir.resolve("graalgo/runtime/Scheduler.class");
            assertTrue(Files.isRegularFile(stubClass),
                    "stub Scheduler.class must exist, else the test is vacuous: " + stubClass);
            String out = runChildMainHelp(stubDir);
            assertTrue(out.contains("FATAL") && out.contains("Phase-1 Scheduler stub"),
                    "child must report the FATAL overlay message; output was: " + out);
        } finally {
            if (stubDir != null) {
                deleteTree(stubDir.getParent()); // the temp root (stub-classes + leftovers)
            }
        }
    }

    /**
     * verifies: R-02 (each remaining shadow fails fast when its stub
     * loads first, never silently). One test per class so a regression
     * names the class: each puts a minimal stub (same FQN, no
     * isPhase2 method — the real truffle stub's distinguishing shape)
     * FIRST on a child JVM's classpath and requires the child to die
     * with the FATAL overlay message naming that class. The stub has
     * no other methods on purpose: the probe's NoSuchMethodError is
     * the whole mechanism, and a stub with the marker would not be a
     * stub at all.
     */
    public void testReversedClasspathFailsFastChanOps() throws Exception {
        testReversedClasspathFailsFastFor("graalgo.runtime.ChanOps");
    }

    /** verifies: R-02 (stub GcInterop loaded first fails fast). */
    public void testReversedClasspathFailsFastGcInterop() throws Exception {
        testReversedClasspathFailsFastFor("graalgo.runtime.GcInterop");
    }

    /** verifies: R-02 (stub GoroutineState loaded first fails fast). */
    public void testReversedClasspathFailsFastGoroutineState() throws Exception {
        testReversedClasspathFailsFastFor("graalgo.runtime.GoroutineState");
    }

    /** verifies: R-02 (stub Netpoll loaded first fails fast). */
    public void testReversedClasspathFailsFastNetpoll() throws Exception {
        testReversedClasspathFailsFastFor("graalgo.runtime.Netpoll");
    }

    /** verifies: R-02 (stub Panic loaded first fails fast). */
    public void testReversedClasspathFailsFastPanic() throws Exception {
        testReversedClasspathFailsFastFor("graalgo.runtime.Panic");
    }

    /** verifies: R-02 (stub Timers loaded first fails fast). */
    public void testReversedClasspathFailsFastTimers() throws Exception {
        testReversedClasspathFailsFastFor("graalgo.runtime.Timers");
    }

    /** verifies: R-02 (stub Safepoint loaded first fails fast). */
    public void testReversedClasspathFailsFastSafepoint() throws Exception {
        testReversedClasspathFailsFastFor("graalgo.safepoint.Safepoint");
    }

    private static void testReversedClasspathFailsFastFor(String fqn) throws Exception {
        Path stubDir = null;
        try {
            stubDir = compileMinimalStub(fqn);
            String simple = fqn.substring(fqn.lastIndexOf('.') + 1);
            Path stubClass = stubDir.resolve(fqn.replace('.', '/') + ".class");
            assertTrue(Files.isRegularFile(stubClass),
                    "stub " + simple + ".class must exist, else the test is vacuous: " + stubClass);
            String out = runChildMainHelp(stubDir);
            assertTrue(out.contains("FATAL") && out.contains(fqn),
                    "child must report the FATAL overlay message naming " + fqn
                    + "; output was: " + out);
        } finally {
            if (stubDir != null) {
                deleteTree(stubDir.getParent()); // the temp root (stub-classes + leftovers)
            }
        }
    }

    /**
     * Runs {@code graalgo.launcher.Main --help} in a child JVM with
     * {@code stubDir} FIRST on the classpath (ahead of the runtime
     * classes and the jars), and returns its merged output. Fails
     * unless the child exits nonzero: a stub-first launch must never
     * print usage and exit 0.
     */
    private static String runChildMainHelp(Path stubDir) throws Exception {
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
            throw new AssertionError("child JVM did not exit within 120s (reversed classpath)");
        }
        assertTrue(p.exitValue() != 0,
                "child with stub first must NOT exit 0; output was: " + out);
        return out;
    }

    /**
     * Compiles a minimal stub for {@code fqn} (same fully-qualified
     * name, private constructor, no methods — in particular no
     * {@code isPhase2}) into a fresh temp directory; returns the
     * classes dir to put first on a child JVM's classpath.
     */
    private static Path compileMinimalStub(String fqn) throws Exception {
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        assertTrue(compiler != null,
                "system JavaCompiler must be available (pinned GraalVM JDK, not a JRE)");
        Path tmpParent = Path.of(System.getProperty("java.io.tmpdir"));
        Files.createDirectories(tmpParent);
        Path dir = Files.createTempDirectory(tmpParent, "graalgo-overlay-test-");
        int dot = fqn.lastIndexOf('.');
        String pkg = fqn.substring(0, dot);
        String simple = fqn.substring(dot + 1);
        Path pkgDir = dir.resolve("src/" + pkg.replace('.', '/'));
        Files.createDirectories(pkgDir);
        Path src = pkgDir.resolve(simple + ".java");
        Files.writeString(src,
                "package " + pkg + ";\n"
                + "public final class " + simple + " {\n"
                + "    private " + simple + "() {}\n"
                + "}\n",
                StandardCharsets.UTF_8);
        Path outDir = dir.resolve("classes");
        Files.createDirectories(outDir);
        int rc = compiler.run(null, null, null,
                "-d", outDir.toString(), src.toString());
        assertTrue(rc == 0, "minimal stub " + fqn + " must compile (javac exit " + rc + ")");
        Path flat = dir.resolve("stub-classes");
        Files.createDirectories(flat);
        Path clsRel = Path.of(pkg.replace('.', '/'), simple + ".class");
        Files.createDirectories(flat.resolve(clsRel.getParent()));
        Files.move(outDir.resolve(clsRel), flat.resolve(clsRel));
        deleteTree(dir.resolve("src"));
        deleteTree(outDir);
        return flat;
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
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        assertTrue(compiler != null,
                "system JavaCompiler must be available (pinned GraalVM JDK, not a JRE)");
        // Self-sufficient temp parent: createTempDirectory does not create a
        // missing parent, and the Makefile's mkdir of out/test-tmp must not
        // be a precondition for running this test directly.
        Path tmpParent = Path.of(System.getProperty("java.io.tmpdir"));
        Files.createDirectories(tmpParent);
        Path dir = Files.createTempDirectory(tmpParent, "graalgo-overlay-test-");
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
