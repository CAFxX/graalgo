package graalgo.frontend;

import graalgo.launcher.Differential;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.stream.Stream;

/**
 * M7 acceptance: the package-level vars corpus, differential vs stock gc.
 *
 * <p>Red-green-first (SPEC §5.2): for each corpus program the expected
 * observables come from stock gc at test time — the test cannot pass by
 * construction. See {@link Differential#checkProgram} for the comparison
 * (exit code, stdout exact, stderr normalized).
 *
 * <p>Corpus conventions: {@code corpus/vars/gv*.go}, each a complete
 * program printing its results with the fmt stub. Run from the phase1
 * directory ({@code make test}); override with
 * {@code -Dgraalgo.phase1.dir=...}.
 */
// verifies: GG-0014 (M7: package-level const/var declarations — const
// folding incl. iota, dependency-order initialization, globals readable
// and writable from every function — execute with gc-identical observable
// behavior).
public final class VarsCorpusTest {

    private static Path phase1Dir() {
        return Path.of(System.getProperty("graalgo.phase1.dir", "."));
    }

    private static Path corpusDir() {
        return phase1Dir().resolve("corpus").resolve("vars");
    }

    private static Path prog(String base) {
        return corpusDir().resolve(base + ".go");
    }

    /** Guards against accidental corpus shrinkage. */
    public void testCorpusHasEnoughPrograms() throws Exception {
        long n;
        try (Stream<Path> s = Files.list(corpusDir())) {
            n = s.map(p -> p.getFileName().toString())
                    .filter(x -> x.startsWith("gv") && x.endsWith(".go"))
                    .count();
        }
        if (n < 9) {
            throw new AssertionError("vars corpus shrank to " + n + " programs");
        }
    }

    public void testGv01Const() throws Exception { Differential.checkProgram(prog("gv01_const")); }
    public void testGv02Iota() throws Exception { Differential.checkProgram(prog("gv02_iota")); }
    public void testGv03VarInit() throws Exception { Differential.checkProgram(prog("gv03_var_init")); }
    public void testGv04VarZero() throws Exception { Differential.checkProgram(prog("gv04_var_zero")); }
    public void testGv05DepOrder() throws Exception { Differential.checkProgram(prog("gv05_dep_order")); }
    public void testGv06VarCall() throws Exception { Differential.checkProgram(prog("gv06_var_call")); }
    public void testGv07GlobalMutate() throws Exception { Differential.checkProgram(prog("gv07_global_mutate")); }
    public void testGv08VarString() throws Exception { Differential.checkProgram(prog("gv08_var_string")); }
    public void testGv09Blank() throws Exception { Differential.checkProgram(prog("gv09_blank")); }
}
