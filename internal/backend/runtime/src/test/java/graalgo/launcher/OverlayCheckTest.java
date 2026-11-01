package graalgo.launcher;

import java.io.File;
import java.io.IOException;
import java.lang.reflect.InvocationTargetException;
import java.net.URL;
import java.nio.file.FileVisitResult;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.SimpleFileVisitor;
import java.nio.file.attribute.BasicFileAttributes;
import java.util.ArrayList;
import java.util.List;

/**
 * Lane C: the shared overlay check in {@link OverlayCheck}.
 *
 * <p>Three layers, each demonstrably capable of failing:
 * <ul>
 *   <li>in-process pass on the runner's own (correct) classpath;</li>
 *   <li>reversed order in a child loader — the truffle stub
 *       {@code Scheduler} loads first, so the canary layer must fire
 *       with the FATAL Phase-1 message (this mirrors
 *       {@code LauncherOverlayTest}, but through the shared check);</li>
 *   <li>split classpath in a child loader — the runtime
 *       {@code Scheduler} loads (canary passes) while truffle
 *       {@code Timers} loads from a different entry, so the
 *       resource-root layer must fire naming {@code Timers}. This is
 *       the failure the old single-canary probe could NOT catch, and
 *       it is the reason the shared check has a second layer.</li>
 * </ul>
 *
 * <p>Fixtures are built from the test JVM's own classpath (the runtime
 * classes dir holds {@code graalgo/launcher/Main.class}; the truffle
 * classes dir holds the stubs but no launcher), never from a hardcoded
 * build path, so the test fails loudly — instead of passing vacuously
 * — when run outside the module's test classpath.
 */
public final class OverlayCheckTest {

    /** verifies: lane C (shared check passes on the correct classpath). */
    public void testOverlayCheckPasses() {
        OverlayCheck.assertOverlayActive();
    }

    /** verifies: lane C (reversed order fails fast via the shared check). */
    public void testReversedClasspathFailsFast() throws Exception {
        Path runtimeDir = runtimeClassesDir();
        Path truffleDir = truffleClassesDir();
        URL[] urls = {
            truffleDir.toUri().toURL(),
            runtimeDir.toUri().toURL(),
        };
        try (java.net.URLClassLoader cl =
                new java.net.URLClassLoader(urls, ClassLoader.getPlatformClassLoader())) {
            try {
                invokeCheck(cl);
            } catch (InvocationTargetException ite) {
                Throwable cause = ite.getCause();
                assertTrue(cause instanceof IllegalStateException,
                        "reversed classpath must fail with IllegalStateException, got: " + cause);
                assertTrue(cause.getMessage().contains("FATAL")
                                && cause.getMessage().contains("Phase-1 Scheduler stub"),
                        "reversed classpath must report the FATAL overlay message; got: "
                        + cause.getMessage());
                return;
            }
            throw new AssertionError(
                    "reversed classpath must NOT pass the overlay check");
        }
    }

    /** verifies: lane C (split classpath fails naming the split class). */
    public void testSplitClasspathFailsFast() throws Exception {
        Path runtimeDir = runtimeClassesDir();
        Path truffleDir = truffleClassesDir();
        Path tmp = null;
        try {
            tmp = newTempDir();
            // dirA: every runtime overlay class (canary included).
            Path dirA = tmp.resolve("runtime-side");
            for (String fqn : OverlayCheck.OVERLAY_CLASSES) {
                copyClass(runtimeDir, dirA, fqn.replace('.', '/') + ".class");
            }
            // dirB: only the truffle Timers — placed BEFORE dirA so
            // Timers resolves here while Scheduler resolves in dirA.
            Path dirB = tmp.resolve("truffle-side");
            copyClass(truffleDir, dirB, "graalgo/runtime/Timers.class");
            // dirC: the check itself, so the child loader's copy runs.
            Path dirC = tmp.resolve("check");
            copyClass(runtimeDir, dirC, "graalgo/launcher/OverlayCheck.class");
            URL[] urls = {
                dirC.toUri().toURL(),
                dirB.toUri().toURL(),
                dirA.toUri().toURL(),
            };
            try (java.net.URLClassLoader cl = new java.net.URLClassLoader(
                    urls, ClassLoader.getPlatformClassLoader())) {
                try {
                    invokeCheck(cl);
                } catch (InvocationTargetException ite) {
                    Throwable cause = ite.getCause();
                    assertTrue(cause instanceof IllegalStateException,
                            "split classpath must fail with IllegalStateException, got: " + cause);
                    assertTrue(cause.getMessage().contains("FATAL")
                                    && cause.getMessage().contains("Timers"),
                            "split classpath must name the split class (Timers); got: "
                            + cause.getMessage());
                    return;
                }
                throw new AssertionError(
                        "split classpath must NOT pass the overlay check");
            }
        } finally {
            if (tmp != null) {
                deleteTree(tmp);
            }
        }
    }

    /** Reflectively runs the child loader's copy of the shared check. */
    private static void invokeCheck(ClassLoader cl) throws Exception {
        Class<?> check = Class.forName("graalgo.launcher.OverlayCheck", true, cl);
        check.getMethod("assertOverlayActive").invoke(null);
    }

    /** Classpath directory holding the runtime module's classes. */
    private static Path runtimeClassesDir() throws Exception {
        for (Path entry : classpathDirs()) {
            if (Files.isRegularFile(entry.resolve("graalgo/launcher/Main.class"))) {
                return entry;
            }
        }
        throw new AssertionError(
                "runtime classes dir (holding graalgo/launcher/Main.class) not on "
                + "the test classpath: " + System.getProperty("java.class.path"));
    }

    /** Classpath directory holding the truffle module's stub classes. */
    private static Path truffleClassesDir() throws Exception {
        for (Path entry : classpathDirs()) {
            if (Files.isRegularFile(entry.resolve("graalgo/runtime/Timers.class"))
                    && !Files.isRegularFile(entry.resolve("graalgo/launcher/Main.class"))) {
                return entry;
            }
        }
        throw new AssertionError(
                "truffle classes dir (holding stubs but no launcher) not on "
                + "the test classpath: " + System.getProperty("java.class.path"));
    }

    /** Test-classpath entries that are directories, resolved absolutely. */
    private static List<Path> classpathDirs() {
        List<Path> dirs = new ArrayList<>();
        Path cwd = Path.of(System.getProperty("user.dir"));
        for (String entry : System.getProperty("java.class.path").split(File.pathSeparator)) {
            Path p = Path.of(entry);
            if (!p.isAbsolute()) {
                p = cwd.resolve(p);
            }
            if (Files.isDirectory(p)) {
                dirs.add(p);
            }
        }
        return dirs;
    }

    private static Path newTempDir() throws IOException {
        Path tmpParent = Path.of(System.getProperty("java.io.tmpdir"));
        Files.createDirectories(tmpParent);
        return Files.createTempDirectory(tmpParent, "graalgo-overlaycheck-test-");
    }

    private static void copyClass(Path fromDir, Path toDir, String rel) throws IOException {
        Path src = fromDir.resolve(rel);
        assertTrue(Files.isRegularFile(src),
                "fixture class must exist, else the test is vacuous: " + src);
        Path dst = toDir.resolve(rel);
        Files.createDirectories(dst.getParent());
        Files.copy(src, dst);
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
}
