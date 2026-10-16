package graalgo.frontend;

import graalgo.launcher.ToolPaths;
import graalgo.values.GoKind;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

/**
 * M5 acceptance: the panic corpus, differential vs stock gc.
 *
 * <p>Red-green-first (SPEC §5.2): for each corpus program the expected
 * behavior comes from stock gc at test time — the test cannot pass by
 * construction. The corpus has two program kinds, distinguished by the
 * trailer comment (see {@code corpus/panic/README.md}):
 *
 * <ul>
 *   <li>{@code // observe: vars...} — recovered programs. The gc oracle
 *       injects {@code defer func() { println(vars...) }()} at the
 *       {@code // probe} anchor (main's first-registered defer, so it runs
 *       <em>last</em> and observes post-defer values), runs {@code go run}
 *       with {@code GOTOOLCHAIN=local GOMAXPROCS=1}, and parses the first
 *       stderr line. The graalgo path is gofrontend → {@link NodeFactory} →
 *       execute on a manually created frame → per-kind frame-slot reads
 *       (deferred calls mutate the frame through the capture chain, so the
 *       slots hold post-defer values). Values are asserted bit-identical.
 *   <li>{@code // panic: <expected first stderr line>} — fatal programs
 *       (unrecovered panic, Goexit in main). The gc side runs
 *       {@code go run}; the graalgo side runs the real
 *       {@code graalgo-run} launcher as a subprocess (the diffharness
 *       invocation shape), so the comparison covers the true CLI
 *       contract: process exit code and exact stderr bytes. The
 *       comparison scope is deliberately narrow (see below).
 * </ul>
 *
 * <p><b>Fatal-program comparison scope</b> (explicit — stderr/exit codes
 * cannot be compared naively):
 * <ol>
 *   <li>Exit code: graalgo-run exits 2 ({@code EXIT_PANIC}); stock gc's
 *       program exit code is 2 as well, observed as {@code go run}'s
 *       {@code exit status 2} trailer line on stderr ({@code go run} itself
 *       exits 1 — that 1 is the driver's code, not the program's, and is
 *       not compared).
 *   <li>Panic header block: the stderr lines up to (excluding) the blank
 *       line before {@code goroutine 1 [running]:} — i.e. the
 *       {@code panic: <value>} line plus gc's optional
 *       {@code [signal SIGSEGV: ...]} line — compared exactly after two
 *       documented normalizations:
 *       <ul>
 *         <li>HotSpot/Truffle {@code sun.misc.Unsafe} terminal-deprecation
 *             {@code WARNING:} lines are dropped from graalgo's stderr
 *             (known M7 concern; the diffharness normalizer allowlists the
 *             same lines).
 *         <li>{@code 0x[0-9a-f]+} addresses are rewritten to
 *             {@code 0xADDR}: gc prints real addresses for composite panic
 *             values and signal pcs; graalgo renders the deterministic
 *             {@code (type) 0xADDR} / {@code pc=0xADDR}.
 *       </ul>
 *   <li>Goroutine trace: only the header line
 *       {@code goroutine 1 [running]:} is asserted present in both outputs
 *       when the header block starts with {@code panic:}. Frame bodies are
 *       <em>not</em> compared: they carry file paths, line numbers, and pc
 *       offsets that differ legitimately (graalgo frames are documented as
 *       graalgo-shaped in {@code Main#reportPanic}).
 *   <li>{@code fatal error:} programs (Goexit in main): gc prints no
 *       goroutine trace for the deadlock fatal error, so only the first
 *       line is compared (graalgo's launcher was aligned to print exactly
 *       gc's line, no header).
 * </ol>
 *
 * <p>The {@code // panic:} trailer is additionally asserted equal to gc's
 * first stderr line, pinning the corpus documentation to the oracle.
 *
 * <p>Run from the phase1 directory ({@code make test}); override with
 * {@code -Dgraalgo.repo.dir=...}.
 */
// verifies: GG-0019 (M5: defer/panic/recover/Goexit differential-green vs
// stock gc — recovered programs assert every observed value bit-identical;
// fatal programs assert exit code 2, the normalized panic header block, and
// the goroutine-trace header; panic(nil) renders as *PanicNilError,
// recover-outside-defer is nil, Goexit-in-main is gc's fatal deadlock).
public final class PanicCorpusTest {

    private static Path repoDir() {
        return Path.of(System.getProperty("graalgo.repo.dir", "."));
    }

    private static Path corpusDir() {
        return repoDir().resolve("test").resolve("corpus").resolve("phase1").resolve("panic");
    }

    private static List<String> programs() throws Exception {
        try (Stream<Path> s = Files.list(corpusDir())) {
            return s.map(p -> p.getFileName().toString())
                    .filter(n -> n.startsWith("pn") && n.endsWith(".go"))
                    .sorted()
                    .map(n -> n.substring(0, n.length() - 3))
                    .toList();
        }
    }

    /** Guards against accidental corpus shrinkage: the adversarial cover below must stay. */
    public void testCorpusHasEnoughPrograms() throws Exception {
        List<String> ps = programs();
        if (ps.size() < 20) {
            throw new AssertionError(
                    "panic corpus shrank to " + ps.size() + " programs: " + ps);
        }
    }

    public void testPn01DeferLifo() throws Exception { checkProgram("pn01_defer_lifo"); }
    public void testPn02DeferArgsEarly() throws Exception { checkProgram("pn02_defer_args_early"); }
    public void testPn03DeferNamedResults() throws Exception { checkProgram("pn03_defer_named_results"); }
    public void testPn04PanicString() throws Exception { checkProgram("pn04_panic_string"); }
    public void testPn05PanicValues() throws Exception { checkProgram("pn05_panic_values"); }
    public void testPn06PanicNil() throws Exception { checkProgram("pn06_panic_nil"); }
    public void testPn07RecoverStops() throws Exception { checkProgram("pn07_recover_stops"); }
    public void testPn08RecoverOutsideDefer() throws Exception { checkProgram("pn08_recover_outside_defer"); }
    public void testPn09NestedPanic() throws Exception { checkProgram("pn09_nested_panic"); }
    public void testPn10Goexit() throws Exception { checkProgram("pn10_goexit"); }
    public void testPn11GoexitNotRecoverable() throws Exception { checkProgram("pn11_goexit_not_recoverable"); }
    public void testPn12DeepRecursion() throws Exception { checkProgram("pn12_deep_recursion"); }
    public void testPn13DeferStorm() throws Exception { checkProgram("pn13_defer_storm"); }
    public void testPn14OobIndex() throws Exception { checkProgram("pn14_oob_index"); }
    public void testPn15Adv06PanicCapture() throws Exception { checkProgram("pn15_adv06_panic_capture"); }
    public void testPn16Adv07Crossframe() throws Exception { checkProgram("pn16_adv07_crossframe"); }
    public void testPn17Adv08RecoverNamed() throws Exception { checkProgram("pn17_adv08_recover_named"); }
    public void testPn18Adv09DeferUnwind() throws Exception { checkProgram("pn18_adv09_defer_unwind"); }
    public void testPn19Adv11GoexitNested() throws Exception { checkProgram("pn19_adv11_goexit_nested"); }
    public void testPn20Adv12RecoverNested() throws Exception { checkProgram("pn20_adv12_recover_nested"); }
    public void testPn15SliceBounds() throws Exception { checkProgram("pn15_slice_bounds"); }
    public void testPn16NilMapWrite() throws Exception { checkProgram("pn16_nil_map_write"); }
    public void testPn17TypeAssert() throws Exception { checkProgram("pn17_type_assert"); }
    public void testPn18NilDeref() throws Exception { checkProgram("pn18_nil_deref"); }
    public void testPn19DivZero() throws Exception { checkProgram("pn19_div_zero"); }
    public void testPn20PanicSliceValue() throws Exception { checkProgram("pn20_panic_slice_value"); }

    /** Dispatches on the trailer kind: {@code // observe:} vs {@code // panic:}. */
    static void checkProgram(String base) throws Exception {
        Path src = corpusDir().resolve(base + ".go");
        String trailer = trailerLine(src);
        if (trailer.startsWith("// observe:")) {
            checkRecovered(base, src);
        } else if (trailer.startsWith("// panic:")) {
            checkFatal(base, src, trailer.substring("// panic:".length()).trim());
        } else {
            throw new AssertionError(src + ": trailer must be // observe: or // panic:, got <"
                    + trailer + ">");
        }
    }

    /** Reads the trailer comment (last //-comment line of the file). */
    static String trailerLine(Path src) throws Exception {
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        for (int i = lines.size() - 1; i >= 0; i--) {
            String t = lines.get(i).trim();
            if (t.startsWith("//")) {
                return t;
            }
        }
        throw new AssertionError(src + ": missing trailer comment");
    }

    // ------------------------------------------------------------------
    // Recovered programs: value comparison via the defer-probe oracle.
    // ------------------------------------------------------------------

    static void checkRecovered(String base, Path src) throws Exception {
        List<String> vars = StraightLineCorpusTest.observeVars(src);
        NodeFactory.Program program = ArithmeticCorpusTest.buildProgram(src);
        List<GoKind> kinds = ArithmeticCorpusTest.kindsFor(program, src, vars);
        List<String> expected = gcOracleProbe(src, vars, kinds);
        List<String> actual = ArithmeticCorpusTest.graalgoValues(program, vars, kinds);
        for (int i = 0; i < vars.size(); i++) {
            if (!expected.get(i).equals(actual.get(i))) {
                throw new AssertionError(base + "." + vars.get(i) + " (" + kinds.get(i) + "): gc=<"
                        + expected.get(i) + "> graalgo=<" + actual.get(i) + ">");
            }
        }
    }

    /**
     * Stock-gc oracle for recovered programs: replaces the {@code // probe}
     * anchor with {@code defer func() { println(dump...) }()} — main's
     * first-registered defer, hence the last to run, so it observes
     * post-defer values — runs {@code go run}, and parses the first stderr
     * line. Dump encodings are kind-aware (ints decimal, floats via
     * {@code math.Float64bits}, strings hex), mirroring
     * {@link ArithmeticCorpusTest}.
     */
    static List<String> gcOracleProbe(Path src, List<String> vars, List<GoKind> kinds)
            throws Exception {
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        boolean needMath = false;
        boolean needHex = false;
        List<String> dump = new ArrayList<>(vars.size());
        for (int i = 0; i < vars.size(); i++) {
            String v = vars.get(i);
            switch (kinds.get(i)) {
                case FLOAT32 -> {
                    needMath = true;
                    dump.add("math.Float32bits(" + v + ")");
                }
                case FLOAT64 -> {
                    needMath = true;
                    dump.add("math.Float64bits(" + v + ")");
                }
                case STRING -> {
                    needHex = true;
                    dump.add("hex.EncodeToString([]byte(" + v + "))");
                }
                default -> dump.add(v);
            }
        }
        String probe = "\tdefer func() { println(" + String.join(", ", dump) + ") }()";
        List<String> out = new ArrayList<>();
        boolean pkgSeen = false;
        boolean probePlaced = false;
        for (String line : lines) {
            // The anchor is "// probe" with an optional ": ..." suffix
            // describing the program (corpus authors may annotate it).
            if (!probePlaced && line.trim().startsWith("// probe")
                    && (line.trim().equals("// probe")
                        || line.trim().startsWith("// probe:"))) {
                out.add(probe);
                probePlaced = true;
                continue;
            }
            out.add(line);
            if (!pkgSeen && line.startsWith("package ")) {
                pkgSeen = true;
                if (needMath) {
                    out.add("import \"math\"");
                }
                if (needHex) {
                    out.add("import \"encoding/hex\"");
                }
            }
        }
        if (!probePlaced) {
            throw new AssertionError(src + ": recovered program is missing the // probe anchor"
                    + " (see corpus/panic/README.md)");
        }
        Path dir = Files.createTempDirectory("graalgo-panic-oracle");
        Path injected = dir.resolve("oracle.go");
        Files.write(injected, out, StandardCharsets.UTF_8);

        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "run", injected.toString());
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.environment().put("GOMAXPROCS", "1");
        pb.redirectErrorStream(false);
        Process p = pb.start();
        String stdout = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        String stderr = new String(p.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(120, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(src + ": stock go run timed out");
        }
        if (p.exitValue() != 0) {
            throw new AssertionError(src + ": stock go run failed (exit " + p.exitValue()
                    + "): stdout=<" + stdout + "> stderr=<" + stderr + ">");
        }
        String firstLine = firstNonEmptyLine(stderr);
        if (firstLine == null) {
            throw new AssertionError(src + ": oracle printed nothing: stderr=<" + stderr + ">");
        }
        String[] parts = firstLine.trim().split("\\s+");
        if (parts.length != vars.size()) {
            throw new AssertionError(src + ": oracle output shape mismatch: <" + firstLine.trim()
                    + "> for vars " + vars);
        }
        return List.of(parts);
    }

    // ------------------------------------------------------------------
    // Fatal programs: exit code + normalized panic header block.
    // ------------------------------------------------------------------

    static void checkFatal(String base, Path src, String trailerExpect) throws Exception {
        GcFatal gc = gcFatal(src);
        // The trailer pins the corpus documentation to the oracle.
        if (!gc.firstLine.equals(trailerExpect)) {
            throw new AssertionError(base + ": // panic: trailer <" + trailerExpect
                    + "> does not match gc's first stderr line <" + gc.firstLine + ">");
        }
        GraalgoFatal actual = graalgoFatal(src);
        if (actual.exitCode != 2) {
            throw new AssertionError(base + ": graalgo exit=" + actual.exitCode
                    + ", expected 2 (gc program exit 2; go run reported <"
                    + gc.lastLine + ">)");
        }
        List<String> wantBlock = headerBlock(gc.lines);
        List<String> gotBlock = headerBlock(actual.lines);
        if (!wantBlock.equals(gotBlock)) {
            throw new AssertionError(base + ": panic header block differs:\n  gc=<"
                    + String.join(" | ", wantBlock) + ">\n  graalgo=<"
                    + String.join(" | ", gotBlock) + ">");
        }
        if (wantBlock.get(0).startsWith("panic:")) {
            assertHasLine(gc.lines, "goroutine 1 [running]:", base, "gc");
            assertHasLine(actual.lines, "goroutine 1 [running]:", base, "graalgo");
        }
    }

    /** Stock-gc fatal run: exit code + stderr lines. */
    static GcFatal gcFatal(Path src) throws Exception {
        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "run", src.toString());
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.environment().put("GOMAXPROCS", "1");
        pb.redirectErrorStream(false);
        Process p = pb.start();
        String stderr = new String(p.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(120, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(src + ": stock go run timed out");
        }
        // go run exits 1 when the program fails; the program's own exit
        // code is the "exit status N" trailer line it prints.
        if (p.exitValue() != 1) {
            throw new AssertionError(src + ": expected go run to exit 1 on a fatal program, got "
                    + p.exitValue() + ": stderr=<" + stderr + ">");
        }
        List<String> lines = nonEmptyLines(stderr);
        if (lines.isEmpty()) {
            throw new AssertionError(src + ": gc printed nothing on stderr");
        }
        String last = lines.get(lines.size() - 1);
        if (!last.equals("exit status 2")) {
            throw new AssertionError(src + ": gc's program exit was not 2: last stderr line=<"
                    + last + "> full=<" + stderr + ">");
        }
        // The "exit status 2" line is go run's wrapper output, not the
        // program's: drop it before comparing.
        lines = lines.subList(0, lines.size() - 1);
        return new GcFatal(lines.get(0), last, lines);
    }

    /**
     * graalgo fatal run: the real {@code graalgo-run} launcher as a
     * subprocess (like the diffharness invokes it), so the comparison
     * covers the true CLI contract — process exit code and the exact
     * stderr bytes, including the HotSpot/Truffle {@code sun.misc.Unsafe}
     * warnings the filter must drop.
     */
    static GraalgoFatal graalgoFatal(Path src) throws Exception {
        Path launcher = repoDir().resolve("internal").resolve("backend").resolve("graalgo-run");
        ProcessBuilder pb = new ProcessBuilder(launcher.toString(), src.toString());
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.environment().put("GOMAXPROCS", "1");
        pb.redirectErrorStream(false);
        Process p = pb.start();
        String stderr = new String(p.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(180, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(src + ": graalgo-run timed out");
        }
        List<String> lines = nonEmptyLines(stderr).stream()
                .filter(PanicCorpusTest::keepStderrLine)
                .map(PanicCorpusTest::normalizeAddresses)
                .toList();
        return new GraalgoFatal(p.exitValue(), lines);
    }

    /**
     * Drops the HotSpot/Truffle {@code sun.misc.Unsafe}
     * terminal-deprecation warnings from graalgo's stderr (known M7
     * concern; the diffharness normalizer allowlists the same lines).
     * Everything else is program output and is compared.
     */
    static boolean keepStderrLine(String line) {
        return !line.startsWith("WARNING: ");
    }

    /**
     * Rewrites {@code 0x[0-9a-f]+} addresses to {@code 0xADDR}: gc prints
     * real addresses for composite panic values ({@code ([]int) 0x...})
     * and signal pcs; graalgo renders the deterministic {@code 0xADDR}.
     */
    static String normalizeAddresses(String line) {
        return line.replaceAll("0x[0-9a-f]+", "0xADDR");
    }

    /**
     * The comparable header block: lines up to (excluding) the blank line
     * before the goroutine trace — the {@code panic:}/{@code fatal error:}
     * line plus gc's optional {@code [signal ...]} line. Frame bodies are
     * never compared (paths/pcs differ legitimately).
     */
    static List<String> headerBlock(List<String> lines) {
        List<String> block = new ArrayList<>();
        for (String l : lines) {
            if (l.equals("goroutine 1 [running]:")) {
                break;
            }
            block.add(normalizeAddresses(l));
        }
        return block;
    }

    static void assertHasLine(List<String> lines, String want, String base, String who) {
        for (String l : lines) {
            if (l.equals(want)) {
                return;
            }
        }
        throw new AssertionError(base + ": " + who + " stderr is missing <" + want + ">");
    }

    static String firstNonEmptyLine(String text) {
        for (String l : text.split("\n")) {
            if (!l.trim().isEmpty()) {
                return l;
            }
        }
        return null;
    }

    static List<String> nonEmptyLines(String text) {
        List<String> out = new ArrayList<>();
        for (String l : text.split("\n")) {
            if (!l.trim().isEmpty()) {
                out.add(l);
            }
        }
        return out;
    }

    /** Stock-gc fatal run result. */
    record GcFatal(String firstLine, String lastLine, List<String> lines) {}

    /** graalgo fatal run result (stderr already filtered + normalized). */
    record GraalgoFatal(int exitCode, List<String> lines) {}
}
