// FastCycleDaemonTest.java — regression test for the per-run classloader
// lifecycle (review-1105-fastcycle-leak): each RUN/RUNTEST must close its
// URLClassLoader and restore the calling thread's context classloader, so
// a long-lived daemon does not retain loaders, their classes, or their
// open jar handles across runs.
//
// Run: internal/backend/fast-cycle/fast-cycle selftest
// (compiles this file + FastCycleDaemon.java with javac and runs main).
// Exits 0 when all checks pass, 1 otherwise. No test framework: the
// daemon is a standalone file outside every module build, so this test
// is a plain main with explicit checks — each check names what it proves
// and fails loudly on the unfixed daemon.
//
// How it observes the loader: the probe classes run inside the daemon's
// per-run URLClassLoader and stash the thread's context classloader into
// ProbeHolder (a nested class of this test, loaded by the system loader,
// so the probe reaches it via Class.forName with the system loader).
// The jar-fd check scans /proc/self/fd (Linux only; skipped elsewhere):
// a closed loader releases its jar handle immediately, an unclosed one
// holds it until GC — so the check runs before any System.gc() call.

import java.io.*;
import java.lang.ref.WeakReference;
import java.nio.file.*;
import java.util.*;
import java.util.jar.*;
import javax.tools.*;

public class FastCycleDaemonTest {
    // Shared with the probe classes (loaded by the per-run loader) via the
    // system classloader: Class.forName("FastCycleDaemonTest$ProbeHolder",
    // true, ClassLoader.getSystemClassLoader()).
    public static class ProbeHolder {
        public static volatile ClassLoader loader;
    }

    static final String PROBE_OK = """
        public class ProbeOk {
            public static int run(String[] args) {
                stash();
                return 0;
            }
            static void stash() {
                try {
                    Class<?> h = Class.forName("FastCycleDaemonTest$ProbeHolder",
                        true, ClassLoader.getSystemClassLoader());
                    h.getField("loader").set(null,
                        Thread.currentThread().getContextClassLoader());
                } catch (Exception e) {
                    throw new RuntimeException(e);
                }
            }
        }
        """;

    static final String PROBE_THROW = """
        public class ProbeThrow {
            public static int run(String[] args) {
                try {
                    Class<?> h = Class.forName("FastCycleDaemonTest$ProbeHolder",
                        true, ClassLoader.getSystemClassLoader());
                    h.getField("loader").set(null,
                        Thread.currentThread().getContextClassLoader());
                } catch (Exception e) {
                    throw new RuntimeException(e);
                }
                throw new RuntimeException("boom from ProbeThrow.run");
            }
        }
        """;

    static final String PROBE_TEST = """
        public class ProbeTest {
            public void testOk() {
                try {
                    Class<?> h = Class.forName("FastCycleDaemonTest$ProbeHolder",
                        true, ClassLoader.getSystemClassLoader());
                    h.getField("loader").set(null,
                        Thread.currentThread().getContextClassLoader());
                } catch (Exception e) {
                    throw new RuntimeException(e);
                }
            }
            public void testFail() {
                testOk();
                throw new RuntimeException("boom from ProbeTest.testFail");
            }
        }
        """;

    static int checks = 0;
    static int failures = 0;

    static void check(String name, boolean cond, String detail) {
        checks++;
        if (cond) {
            System.out.println("PASS " + name);
        } else {
            failures++;
            System.out.println("FAIL " + name + " — " + detail);
        }
    }

    // Open fds pointing at the jar (a leaked URLClassLoader keeps it open).
    // Returns -1 when /proc/self/fd is unavailable (non-Linux: skip).
    static int openJarFds(String jar) throws Exception {
        Path fdDir = Path.of("/proc/self/fd");
        if (!Files.isDirectory(fdDir)) return -1;
        int n = 0;
        try (DirectoryStream<Path> ds = Files.newDirectoryStream(fdDir)) {
            for (Path p : ds) {
                try {
                    if (Files.readSymbolicLink(p).toString().equals(jar)) n++;
                } catch (Exception ignored) {
                    // fd vanished mid-scan, or is not a symlink
                }
            }
        }
        return n;
    }

    static void checkJarClosed(String name, String jar) throws Exception {
        int n = openJarFds(jar);
        if (n < 0) {
            System.out.println("SKIP " + name + " (no /proc/self/fd on this platform)");
        } else {
            check(name, n == 0, "jar still open on " + n + " fd(s) after the run");
        }
    }

    public static void main(String[] args) throws Exception {
        List<WeakReference<ClassLoader>> loaders = new ArrayList<>();
        Path tmp = Files.createTempDirectory("fastcycle-daemon-test");
        try {
            JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
            if (compiler == null) {
                System.out.println("FAIL setup — no system Java compiler (not a JDK?)");
                System.exit(1);
            }
            Path srcDir = tmp.resolve("src");
            Path classesDir = tmp.resolve("classes");
            Files.createDirectories(srcDir);
            Files.createDirectories(classesDir);
            Files.writeString(srcDir.resolve("ProbeOk.java"), PROBE_OK);
            Files.writeString(srcDir.resolve("ProbeThrow.java"), PROBE_THROW);
            Files.writeString(srcDir.resolve("ProbeTest.java"), PROBE_TEST);
            DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
            try (StandardJavaFileManager fm =
                     compiler.getStandardFileManager(diagnostics, null, null)) {
                Iterable<? extends JavaFileObject> units = fm.getJavaFileObjectsFromStrings(
                    List.of(srcDir.resolve("ProbeOk.java").toString(),
                        srcDir.resolve("ProbeThrow.java").toString(),
                        srcDir.resolve("ProbeTest.java").toString()));
                JavaCompiler.CompilationTask task = compiler.getTask(null, fm,
                    diagnostics, List.of("-d", classesDir.toString()), null, units);
                if (!task.call()) {
                    for (Diagnostic<? extends JavaFileObject> d : diagnostics.getDiagnostics()) {
                        System.out.println(d);
                    }
                    System.out.println("FAIL setup — probe compilation failed");
                    System.exit(1);
                }
            }
            Path jar = tmp.resolve("probe.jar");
            try (JarOutputStream jos = new JarOutputStream(Files.newOutputStream(jar))) {
                for (String name : List.of("ProbeOk.class", "ProbeThrow.class", "ProbeTest.class")) {
                    jos.putNextEntry(new JarEntry(name));
                    Files.copy(classesDir.resolve(name), jos);
                    jos.closeEntry();
                }
            }
            String cp = jar.toString();
            ClassLoader appLoader = FastCycleDaemonTest.class.getClassLoader();

            // 1. doRun, success path: TCCL restored, loader closed.
            ProbeHolder.loader = null;
            ClassLoader tcclBefore = Thread.currentThread().getContextClassLoader();
            String r = FastCycleDaemon.doRun(cp, "ProbeOk", new String[0]);
            ClassLoader used = ProbeHolder.loader;
            loaders.add(new WeakReference<>(used));
            check("doRun success returns EXIT:0", r.startsWith("EXIT:0"), "got: " + r);
            check("doRun restores TCCL on success",
                Thread.currentThread().getContextClassLoader() == tcclBefore,
                "TCCL after=" + Thread.currentThread().getContextClassLoader()
                    + " before=" + tcclBefore);
            check("doRun ran under a fresh loader (not the app loader)",
                used != null && used != appLoader, "loader=" + used);
            checkJarClosed("doRun closes the loader (jar fd released)", cp);

            // 2. doRun, throwing run(): TCCL restored, loader closed.
            ProbeHolder.loader = null;
            tcclBefore = Thread.currentThread().getContextClassLoader();
            r = FastCycleDaemon.doRun(cp, "ProbeThrow", new String[0]);
            used = ProbeHolder.loader;
            loaders.add(new WeakReference<>(used));
            check("doRun throwing run returns EXIT:1", r.startsWith("EXIT:1"), "got: " + r);
            check("doRun restores TCCL when run() throws",
                Thread.currentThread().getContextClassLoader() == tcclBefore,
                "TCCL after=" + Thread.currentThread().getContextClassLoader()
                    + " before=" + tcclBefore);
            check("doRun throwing run used a fresh loader",
                used != null && used != appLoader, "loader=" + used);
            checkJarClosed("doRun closes the loader when run() throws", cp);

            // 3. doRunTest, success path.
            ProbeHolder.loader = null;
            tcclBefore = Thread.currentThread().getContextClassLoader();
            r = FastCycleDaemon.doRunTest(cp, "ProbeTest", "testOk", Map.of());
            used = ProbeHolder.loader;
            loaders.add(new WeakReference<>(used));
            check("doRunTest success returns EXIT:0", r.startsWith("EXIT:0"), "got: " + r);
            check("doRunTest restores TCCL on success",
                Thread.currentThread().getContextClassLoader() == tcclBefore,
                "TCCL after=" + Thread.currentThread().getContextClassLoader()
                    + " before=" + tcclBefore);
            checkJarClosed("doRunTest closes the loader (jar fd released)", cp);

            // 4. doRunTest, failing test method.
            ProbeHolder.loader = null;
            tcclBefore = Thread.currentThread().getContextClassLoader();
            r = FastCycleDaemon.doRunTest(cp, "ProbeTest", "testFail", Map.of());
            used = ProbeHolder.loader;
            loaders.add(new WeakReference<>(used));
            check("doRunTest failing test returns EXIT:1", r.startsWith("EXIT:1"), "got: " + r);
            check("doRunTest restores TCCL when a test fails",
                Thread.currentThread().getContextClassLoader() == tcclBefore,
                "TCCL after=" + Thread.currentThread().getContextClassLoader()
                    + " before=" + tcclBefore);
            checkJarClosed("doRunTest closes the loader when a test fails", cp);

            // 5. Collectability: once TCCL is restored and the loader
            // closed, nothing retains the per-run loaders — after dropping
            // our own reference they must become GC-eligible.
            ProbeHolder.loader = null;
            used = null;
            for (int i = 0; i < 10; i++) {
                System.gc();
                Thread.sleep(50);
            }
            int cleared = 0;
            for (WeakReference<ClassLoader> w : loaders) {
                if (w.get() == null) cleared++;
            }
            check("per-run loaders become collectable after the run",
                cleared == loaders.size(),
                cleared + "/" + loaders.size() + " loaders collected after GC");
        } finally {
            deleteTree(tmp);
        }

        System.out.println(checks + " checks, " + failures + " failures");
        if (failures > 0) System.exit(1);
    }

    static void deleteTree(Path dir) {
        try {
            if (!Files.exists(dir)) return;
            try (var walk = Files.walk(dir)) {
                walk.sorted(Comparator.reverseOrder()).forEach(p -> {
                    try {
                        Files.deleteIfExists(p);
                    } catch (IOException ignored) {
                        // best effort; temp dirs are reaped by the OS
                    }
                });
            }
        } catch (IOException ignored) {
            // best effort
        }
    }
}
