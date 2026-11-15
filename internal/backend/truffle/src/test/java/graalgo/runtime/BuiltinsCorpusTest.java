package graalgo.runtime;

import graalgo.launcher.Differential;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.stream.Stream;

/**
 * M7 acceptance: the builtins corpus, differential vs stock gc.
 *
 * <p>The {@code corpus/builtins} programs document gc's expected outputs in
 * their README but had no automated differential driver — this test closes
 * that gap. Red-green-first (SPEC §5.2): for each program the expected
 * observables come from stock gc at test time. See
 * {@link Differential#checkProgram} for the comparison (exit code, stdout
 * exact, stderr normalized).
 *
 * <p>Run from the phase1 directory ({@code make test}); override with
 * {@code -Dgraalgo.phase1.dir=...}.
 */
// verifies: GG-0020 (M7: builtin functions — append/copy/delete/len/cap/
// make/new/complex/real/imag/print/println — execute with gc-identical
// observable behavior on the builtins corpus).
public final class BuiltinsCorpusTest {

    private static Path repoDir() {
        return Path.of(System.getProperty("graalgo.repo.dir", "."));
    }

    private static Path corpusDir() {
        return repoDir().resolve("test").resolve("corpus").resolve("phase1").resolve("builtins");
    }

    private static Path prog(String base) {
        return corpusDir().resolve(base + ".go");
    }

    /** Guards against accidental corpus shrinkage. */
    public void testCorpusHasEnoughPrograms() throws Exception {
        long n;
        try (Stream<Path> s = Files.list(corpusDir())) {
            n = s.map(p -> p.getFileName().toString())
                    .filter(x -> x.startsWith("bi") && x.endsWith(".go"))
                    .count();
        }
        if (n < 15) {
            throw new AssertionError("builtins corpus shrank to " + n + " programs");
        }
    }

    public void testBi01AppendGrowth() throws Exception { Differential.checkProgram(prog("bi01_append_growth")); }
    public void testBi02AppendAlias() throws Exception { Differential.checkProgram(prog("bi02_append_alias")); }
    public void testBi03CopyOverlap() throws Exception { Differential.checkProgram(prog("bi03_copy_overlap")); }
    public void testBi04DeleteLookup() throws Exception { Differential.checkProgram(prog("bi04_delete_lookup")); }
    public void testBi05MakeLenCap() throws Exception { Differential.checkProgram(prog("bi05_make_lencap")); }
    public void testBi06Complex() throws Exception { Differential.checkProgram(prog("bi06_complex")); }
    public void testBi07PrintStderr() throws Exception { Differential.checkProgram(prog("bi07_print_stderr")); }
    public void testBi08LenCap() throws Exception { Differential.checkProgram(prog("bi08_len_cap")); }
    public void testBi09New() throws Exception { Differential.checkProgram(prog("bi09_new")); }
    public void testBi10AppendVarieties() throws Exception { Differential.checkProgram(prog("bi10_append_varieties")); }
    public void testBi11Panic() throws Exception { Differential.checkProgram(prog("bi11_panic")); }
    public void testBi12FmtStub() throws Exception { Differential.checkProgram(prog("bi12_fmt_stub")); }
    public void testBi13AppendSizes() throws Exception { Differential.checkProgram(prog("bi13_append_sizes")); }
    public void testBi14MapLifecycle() throws Exception { Differential.checkProgram(prog("bi14_map_lifecycle")); }
    public void testBi15CopyCounts() throws Exception { Differential.checkProgram(prog("bi15_copy_counts")); }
    public void testBi16AppendOverlap() throws Exception { Differential.checkProgram(prog("bi16_append_overlap")); }
    public void testBi17NestedArrays() throws Exception { Differential.checkProgram(prog("bi17_nested_arrays")); }
    public void testBi18StringRune() throws Exception { Differential.checkProgram(prog("bi18_string_rune")); }
    public void testBi19VariadicSpread() throws Exception { Differential.checkProgram(prog("bi19_variadic_spread")); }
    public void testBi20StructPrint() throws Exception { Differential.checkProgram(prog("bi20_struct_print")); }
    public void testBi21NestedMaps() throws Exception { Differential.checkProgram(prog("bi21_nested_maps")); }
    public void testBi22FloatEdge() throws Exception { Differential.checkProgram(prog("bi22_float_edge")); }

    // fix-redmaster-r26 (review F1 on a158aaa6): the spanning carrier read
    // *(*int64)(unsafe.Pointer(&b[0])) over []byte is defined under gc (the
    // 8 bytes read little-endian) but has no flat memory behind it in
    // graalgo's model — the byte[] carrier holds one byte per element.
    // graalgo must trap loudly (UnsafeEscapeException, exit 1) instead of
    // returning the single byte zero-extended. DELIBERATE divergence from
    // gc: this test asserts the trap, not parity, so it cannot use
    // Differential.checkProgram. Falsifiability: pre-guard graalgo exits 0
    // printing 1 (probe r26-probe-pre.log), so the exit-1 assertion fails
    // without the guard.
    public void testBi23SpanningCarrierTrap() throws Exception {
        Path src = prog("bi23_spanning_carrier_trap");
        Differential.GcResult gc = Differential.runGc(src);
        if (gc.exit() != 0) {
            throw new AssertionError(src + ": gc oracle failed (exit " + gc.exit() + ")");
        }
        if (!"578437695752307201\n".equals(gc.stdout())) {
            throw new AssertionError(src + ": gc oracle stdout changed: " + gc.stdout());
        }
        Differential.GraalgoResult g = Differential.runGraalgo(src);
        if (g.exit() != 1) {
            throw new AssertionError(src + ": graalgo must trap with exit 1, got " + g.exit()
                    + "\n--- stdout ---\n" + g.stdout() + "\n--- stderr ---\n" + g.stderr());
        }
        if (!g.stdout().isEmpty()) {
            throw new AssertionError(src + ": graalgo must print nothing on stdout, got: " + g.stdout());
        }
        if (!g.stderr().contains("UnsafeEscapeException")) {
            throw new AssertionError(src + ": graalgo stderr must name UnsafeEscapeException, got: " + g.stderr());
        }
    }

    // fix-redmaster-r27 (review F1 on 24d87358): the spanning
    // unsafe.Pointer read *(*unsafe.Pointer)(unsafe.Pointer(&b[0])) over
    // []byte is defined under gc (the 8 bytes read little-endian as the
    // word, 578437695752307201) but has no flat memory behind it in
    // graalgo's model — the byte[] carrier holds one byte per element.
    // graalgo must trap loudly (UnsafeEscapeException, exit 1) instead of
    // returning the single byte as the word (1). DELIBERATE divergence
    // from gc: this test asserts the trap, not parity, so it cannot use
    // Differential.checkProgram. Falsifiability: pre-guard graalgo exits 0
    // printing "uptr-word: 1" (probe r27-p4-pre.log), so the exit-1
    // assertion fails without the guard.
    public void testBi24SpanningUptrTrap() throws Exception {
        Path src = prog("bi24_spanning_uptr_trap");
        Differential.GcResult gc = Differential.runGc(src);
        if (gc.exit() != 0) {
            throw new AssertionError(src + ": gc oracle failed (exit " + gc.exit() + ")");
        }
        if (!"uptr-word: 578437695752307201\n".equals(gc.stdout())) {
            throw new AssertionError(src + ": gc oracle stdout changed: " + gc.stdout());
        }
        Differential.GraalgoResult g = Differential.runGraalgo(src);
        if (g.exit() != 1) {
            throw new AssertionError(src + ": graalgo must trap with exit 1, got " + g.exit()
                    + "\n--- stdout ---\n" + g.stdout() + "\n--- stderr ---\n" + g.stderr());
        }
        if (!g.stdout().isEmpty()) {
            throw new AssertionError(src + ": graalgo must print nothing on stdout, got: " + g.stdout());
        }
        if (!g.stderr().contains("UnsafeEscapeException")) {
            throw new AssertionError(src + ": graalgo stderr must name UnsafeEscapeException, got: " + g.stderr());
        }
    }
}
