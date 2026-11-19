package graalgo.launcher;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.FileVisitOption;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.stream.Stream;

/**
 * Eager class preloading (M10 P3-M10 WS J, self-attack D3).
 *
 * <p>The JVM loads classes lazily. If the guest program exhausts the
 * process's file descriptors (EMFILE), a first-execution class load —
 * often on an error-reporting path, which is exactly what runs under
 * exhaustion — fails with {@link NoClassDefFoundError} and kills the
 * run, even though the guest itself handled EMFILE correctly. Preloading
 * every {@code graalgo.*} class at launcher startup (while fds are
 * plentiful) removes lazy loading from the failure surface: under fd
 * exhaustion the guest sees clean EMFILEs and keeps running.
 *
 * <p>Loading is done <em>without</em> initialization ({@link Class#forName}
 * with {@code initialize=false}), so no static initializers run early and
 * the preload has no semantic effect — it only pays the class-file parse
 * cost up front. Best-effort: a class that cannot preload now will fail
 * the same way on the normal lazy path later, so failures are swallowed
 * here and surface at their real use site.
 *
 * <p>Only classpath <em>directories</em> are walked (the project's own
 * {@code out/classes} trees); third-party jars are skipped — their
 * classes are either already loaded or not on the exhaustion path.
 */
public final class ClassPreloader {
    private ClassPreloader() {}

    /**
     * Truffle runtime classes whose nested holder classes are initialized
     * lazily from the guest call path (e.g. {@code ReturnProfile}, first
     * initialized when an exception unwinds through a call target).
     */
    private static final java.util.List<String> CALL_PATH_CLASSES = java.util.List.of(
            "com.oracle.truffle.runtime.OptimizedCallTarget");

    /** The classes whose nested holders {@link #initializeCallPathHolders} initializes. */
    public static java.util.List<String> callPathClasses() {
        return CALL_PATH_CLASSES;
    }

    /**
     * Initializes the lazily-initialized holder classes of the Truffle
     * call path on the launcher's shallow stack, before any guest code runs.
     *
     * <p>Why: a class initializer that first runs deep inside guest
     * recursion can hit {@link StackOverflowError}; the JVM then marks the
     * class erroneous for the rest of the process, and every later use
     * throws {@code NoClassDefFoundError: Could not initialize class} —
     * including Truffle's own exception profiling on the way out of the
     * call target, which turned a clean Go "stack overflow" fatal error into
     * a leaked JVM trace (CI, 2026-11-18, {@code OptimizedCallTarget$ReturnProfile}).
     * Initializing these holders up front removes class initialization from
     * the stack-exhaustion failure surface. The holders only build constant
     * sentinels; initializing them early has no semantic effect.
     *
     * <p>Best-effort: a runtime without these classes (another Truffle
     * runtime) skips them; {@code CallPathWarmupTest} pins that the current
     * runtime's holders are initialized.
     */
    public static void initializeCallPathHolders() {
        // The runtime must be initialized first: it opens the compiler
        // module's packages to the runtime classes, which cannot even be
        // loaded before that. Creating the polyglot Context initializes it
        // anyway, so this only moves that work earlier.
        ClassLoader loader;
        try {
            loader = com.oracle.truffle.api.Truffle.getRuntime().getClass().getClassLoader();
        } catch (Throwable t) {
            return;
        }
        for (String outer : CALL_PATH_CLASSES) {
            Class<?> c;
            try {
                c = Class.forName(outer, false, loader);
            } catch (Throwable t) {
                continue;
            }
            for (Class<?> nested : c.getDeclaredClasses()) {
                try {
                    Class.forName(nested.getName(), true, loader);
                } catch (Throwable t) {
                    // Best-effort (see method javadoc).
                }
            }
        }
    }

    /**
     * Initializes the Truffle call-path holders
     * ({@link #initializeCallPathHolders}), then preloads all
     * {@code graalgo.*} classes found on the classpath.
     */
    public static void preload() {
        initializeCallPathHolders();
        String cp = System.getProperty("java.class.path");
        if (cp == null || cp.isEmpty()) {
            return;
        }
        ClassLoader loader = ClassPreloader.class.getClassLoader();
        for (String entry : cp.split(java.io.File.pathSeparator)) {
            Path p;
            try {
                p = Paths.get(entry);
            } catch (Exception e) {
                continue;
            }
            if (!Files.isDirectory(p)) {
                continue;
            }
            try (Stream<Path> s = Files.walk(p, FileVisitOption.FOLLOW_LINKS)) {
                s.filter(f -> f.getFileName().toString().endsWith(".class"))
                        .forEach(f -> preloadOne(p, f, loader));
            } catch (IOException | UncheckedIOException e) {
                // Best-effort: the lazy path reports real problems later.
            }
        }
    }

    private static void preloadOne(Path root, Path file, ClassLoader loader) {
        Path rel = root.relativize(file);
        String name = rel.toString().replace('/', '.').replace('\\', '.');
        if (!name.endsWith(".class") || !name.startsWith("graalgo.")) {
            return;
        }
        name = name.substring(0, name.length() - ".class".length());
        if (name.equals("module-info") || name.endsWith(".module-info")) {
            return;
        }
        try {
            Class.forName(name, false, loader);
        } catch (Throwable t) {
            // Best-effort (see class javadoc).
        }
    }
}
