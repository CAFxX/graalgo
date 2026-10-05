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
 * <p>Each test runs the Go program in {@code phases/phase2/corpus/gc/<name>.go}
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

    private static final Path CORPUS = Paths.get("corpus/gc");
    private static final List<String> PROGRAMS = List.of(
            "gc01_finalizer",
            "gc02_finalizer_remove",
            "gc03_keepalive",
            "gc04_gc_hint",
            "gc05_finalizer_replace");

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
        String stdout;
        String stderr;
        try (var out = p.getInputStream(); var err = p.getErrorStream()) {
            stdout = new String(out.readAllBytes(), StandardCharsets.UTF_8);
            stderr = new String(err.readAllBytes(), StandardCharsets.UTF_8);
        }
        boolean done = p.waitFor(300, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(name + ": subprocess timed out\nstderr:\n" + stderr);
        }
        if (p.exitValue() != 0) {
            throw new AssertionError(name + ": subprocess exit " + p.exitValue()
                    + "\nstdout:\n" + stdout + "\nstderr:\n" + stderr);
        }
        // Strip JVM warnings (e.g. HotSpot notices) that may precede program
        // output. Filter only lines matching known warning patterns; keep all
        // program output lines intact.
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
            if (!Files.exists(CORPUS.resolve(g + ".expected"))) {
                throw new AssertionError("corpus " + g + ".go has no .expected file");
            }
            if (!PROGRAMS.contains(g)) {
                throw new AssertionError("corpus " + g + ".go has no registered test");
            }
        }
        for (String p : PROGRAMS) {
            if (!gos.contains(p)) {
                throw new AssertionError("registered test " + p + " has no .go file");
            }
        }
    }
}
