package graalgo.runtime;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

/**
 * P2-M4 GC-interop differential tests (WS D; ADR-0002 D4).
 *
 * <p>Each test runs the Go program in {@code test/corpus/phase2/gc/<name>.go}
 * end-to-end through {@code SchedMain} (gofrontend → exporter with
 * {@code gcinterop:} markers → interpreter → {@link GcInterop}) as a
 * subprocess, and diffs stdout byte-for-byte against the gc-recorded
 * {@code <name>.expected} file (Go 1.27.1, {@code go run}).
 *
 * <p>Unlike the M1–M3 Java-translation differentials, these exercise the
 * real exporter path: the {@code runtime.SetFinalizer}/{@code KeepAlive}/
 * {@code GC} calls must survive export lowering and dispatch to
 * {@link GcInterop} at runtime.
 *
 * <p>verifies: WS D (ADR-0002 D4 differential)
 */
public final class GcDifferentialTest {

    private static final Path CORPUS = Paths.get("../../../test/corpus/phase2/gc");
    private static final List<String> PROGRAMS = List.of(
            "gc01_finalizer",
            "gc02_finalizer_remove",
            "gc03_keepalive",
            "gc04_gc_hint",
            "gc05_finalizer_replace",
            "gc08_iface_method_dispatch");
    /** Programs with graalgo-specific (non-gc-differential) assertions. */
    private static final List<String> NON_DIFFERENTIAL = List.of(
            "gc06_finalizer_panic",
            "gc07_finalizer_gated");

    /** Stdout + stderr + exit code from a subprocess run. */
    private static final class ProcessResult {
        final String stdout;
        final String stderr;
        final int exitCode;
        ProcessResult(String stdout, String stderr, int exitCode) {
            this.stdout = stdout;
            this.stderr = stderr;
            this.exitCode = exitCode;
        }
    }

    private static void assertEquals(String expected, String actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + "\nexpected:\n" + expected + "\nactual:\n" + actual);
        }
    }

    private String expected(String name) throws Exception {
        return Files.readString(CORPUS.resolve(name + ".expected"), StandardCharsets.UTF_8);
    }

    /**
     * Runs {@code SchedMain --gmp=1 <file>} in a subprocess and returns
     * stdout. Uses the current JVM and classpath so the test works wherever
     * {@code make test} runs it.
     */
    private String runGo(String name) throws Exception {
        ProcessResult r = runGoFull(name);
        if (r.exitCode != 0) {
            throw new AssertionError(name + ": subprocess exit " + r.exitCode
                    + "\nstdout:\n" + r.stdout + "\nstderr:\n" + r.stderr);
        }
        return r.stdout;
    }

    /**
     * Runs {@code SchedMain --gmp=1 <file>} and returns stdout/stderr/exit.
     * Reads both streams concurrently to avoid pipe-buffer deadlock.
     */
    private ProcessResult runGoFull(String name) throws Exception {
        Path goFile = CORPUS.resolve(name + ".go").toAbsolutePath();
        String javaBin = Paths.get(System.getProperty("java.home"), "bin", "java").toString();
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
        pb.redirectErrorStream(false);
        Process p = pb.start();
        // Read both streams concurrently to avoid deadlock.
        java.util.concurrent.Future<String> outF = readAsync(p.getInputStream());
        java.util.concurrent.Future<String> errF = readAsync(p.getErrorStream());
        boolean done = p.waitFor(300, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(name + ": subprocess timed out");
        }
        String stdout;
        String stderr;
        try {
            stdout = outF.get(10, TimeUnit.SECONDS);
            stderr = errF.get(10, TimeUnit.SECONDS);
        } catch (Exception e) {
            throw new AssertionError(name + ": failed to read subprocess output", e);
        }
        return new ProcessResult(stripWarnings(stdout), stderr, p.exitValue());
    }

    private static java.util.concurrent.Future<String> readAsync(java.io.InputStream in) {
        java.util.concurrent.ExecutorService ex =
                java.util.concurrent.Executors.newSingleThreadExecutor();
        java.util.concurrent.Future<String> f = ex.submit(() -> {
            try (var stream = in) {
                return new String(stream.readAllBytes(), StandardCharsets.UTF_8);
            }
        });
        ex.shutdown();
        return f;
    }

    /** Strip JVM warning lines from stdout; keep program output intact. */
    private static String stripWarnings(String stdout) {
        String[] lines = stdout.split("\n", -1);
        StringBuilder sb = new StringBuilder();
        for (String line : lines) {
            if (line.startsWith("Java HotSpot(TM)")
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

    public void testGc01Finalizer() throws Exception {
        assertEquals(expected("gc01_finalizer"), runGo("gc01_finalizer"), "gc01_finalizer");
    }

    public void testGc02FinalizerRemove() throws Exception {
        assertEquals(expected("gc02_finalizer_remove"), runGo("gc02_finalizer_remove"),
                "gc02_finalizer_remove");
    }

    public void testGc03KeepAlive() throws Exception {
        assertEquals(expected("gc03_keepalive"), runGo("gc03_keepalive"), "gc03_keepalive");
    }

    public void testGc04GcHint() throws Exception {
        assertEquals(expected("gc04_gc_hint"), runGo("gc04_gc_hint"), "gc04_gc_hint");
    }

    public void testGc05FinalizerReplace() throws Exception {
        assertEquals(expected("gc05_finalizer_replace"), runGo("gc05_finalizer_replace"),
                "gc05_finalizer_replace");
    }

    /**
     * gc08 (fix-redmaster-r53, review-redmaster-r52 N1): an interface
     * method call through SchedMain. The call emits the "ifacemethod"
     * wire kind, which only the Phase-3 provider table registered by
     * the r52 ServiceLoader block provides; removing that block makes
     * this test fail at AST build time ("unsupported expression kind
     * ifacemethod") instead of regressing silently. The snapshot
     * corpus cannot pin this: it runs through P3SyncMain, not
     * SchedMain.
     */
    public void testGc08IfaceMethodDispatch() throws Exception {
        assertEquals(expected("gc08_iface_method_dispatch"), runGo("gc08_iface_method_dispatch"),
                "gc08_iface_method_dispatch");
    }

    /**
     * gc06: a finalizer that dereferences its nil argument must be LOUD.
     * (DIV-M4-1: not silent.) stdout must be "survived"; stderr must contain
     * the "graalgo: finalizer panicked:" diagnostic. No .expected file —
     * gc would fatal-crash here, so this is graalgo-specific behavior.
     */
    public void testGc06FinalizerPanicIsLoud() throws Exception {
        ProcessResult r = runGoFull("gc06_finalizer_panic");
        assertEquals("survived\n", r.stdout, "gc06 stdout");
        if (!r.stderr.contains("graalgo: finalizer panicked:")) {
            throw new AssertionError(
                    "gc06: expected loud finalizer-panic diagnostic on stderr, got:\n" + r.stderr);
        }
    }

    /**
     * gc07: finalizers are gated on an explicit runtime.GC() (DIV-M4-10).
     * Allocation pressure without the explicit call must NOT run the
     * finalizer (PINNED is a strong root — deterministic); after
     * runtime.GC() releases the pin, the finalizer must run. No .expected
     * file — gc finalizes without the explicit call, so this is
     * graalgo-specific behavior.
     */
    public void testGc07FinalizerGatedOnExplicitGc() throws Exception {
        ProcessResult r = runGoFull("gc07_finalizer_gated");
        assertEquals("gated\nreleased\n", r.stdout, "gc07 stdout");
    }

    /**
     * Lists the corpus directory to ensure every .go has a .expected and a
     * registered test (no silent corpus drift).
     */
    public void testCorpusComplete() throws Exception {
        List<String> gos;
        try (Stream<Path> s = Files.list(CORPUS)) {
            gos = s.filter(p -> p.toString().endsWith(".go"))
                    .map(p -> p.getFileName().toString().replace(".go", ""))
                    .sorted()
                    .toList();
        }
        for (String g : gos) {
            boolean isDifferential = PROGRAMS.contains(g);
            boolean isNonDifferential = NON_DIFFERENTIAL.contains(g);
            if (!isDifferential && !isNonDifferential) {
                throw new AssertionError("corpus " + g + ".go has no registered test");
            }
            if (isDifferential && !Files.exists(CORPUS.resolve(g + ".expected"))) {
                throw new AssertionError("corpus " + g + ".go has no .expected file");
            }
        }
        for (String p : PROGRAMS) {
            if (!gos.contains(p)) {
                throw new AssertionError("registered test " + p + " has no .go file");
            }
        }
    }
}
