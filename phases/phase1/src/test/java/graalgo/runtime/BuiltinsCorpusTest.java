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

    private static Path phase1Dir() {
        return Path.of(System.getProperty("graalgo.phase1.dir", "."));
    }

    private static Path corpusDir() {
        return phase1Dir().resolve("corpus").resolve("builtins");
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
}
