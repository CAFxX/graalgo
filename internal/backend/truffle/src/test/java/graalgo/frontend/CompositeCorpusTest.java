package graalgo.frontend;

/**
 * M4 acceptance (WS E): the composite-types corpus, differential vs stock gc.
 *
 * <p>Red-green-first (SPEC §5.2): for each corpus program the expected
 * values come from stock gc at test time — the test cannot pass by
 * construction. Composite values (slices, maps, structs) are reduced to
 * scalar observables inside the corpus programs (sums, counts, field
 * projections via range loops — no builtins), so the comparison is the same
 * bit-identical {@code long[]} idiom as the other corpus tests. Map
 * reductions are order-independent by construction (commutative sums), so
 * gc's randomized iteration order can never cause a false mismatch.
 *
 * <p>Corpus: {@code corpus/composite/co*.go}, each ending with a
 * {@code // observe:} trailer.
 */
// verifies: GG-0014 (M4: composite types — literals, indexing, slicing,
// field access incl. promotion, comma-ok, type assertions — execute with
// gc-identical observable behavior).
public final class CompositeCorpusTest {

    /** Guards against accidental corpus shrinkage. */
    public void testCorpusHasEnoughPrograms() throws Exception {
        long n;
        try (java.util.stream.Stream<java.nio.file.Path> s =
                java.nio.file.Files.list(java.nio.file.Path.of(
                        System.getProperty("graalgo.phase1.dir", "."),
                        "corpus", "composite"))) {
            n = s.map(p -> p.getFileName().toString())
                    .filter(x -> x.startsWith("co") && x.endsWith(".go"))
                    .count();
        }
        if (n < 10) {
            throw new AssertionError("composite corpus shrank to " + n + " programs");
        }
    }

    public void testCo01Array() throws Exception { checkProgram("co01_array"); }
    public void testCo02Slice() throws Exception { checkProgram("co02_slice"); }
    public void testCo03Slice3() throws Exception { checkProgram("co03_slice3"); }
    public void testCo04Struct() throws Exception { checkProgram("co04_struct"); }
    public void testCo05Embed() throws Exception { checkProgram("co05_embed"); }
    public void testCo06Map() throws Exception { checkProgram("co06_map"); }
    public void testCo07CommaOk() throws Exception { checkProgram("co07_commaok"); }
    public void testCo08Multi() throws Exception { checkProgram("co08_multi"); }
    public void testCo09String() throws Exception { checkProgram("co09_string"); }
    public void testCo10Assert() throws Exception { checkProgram("co10_assert"); }

    static void checkProgram(String base) throws Exception {
        java.nio.file.Path src = java.nio.file.Path.of(
                System.getProperty("graalgo.phase1.dir", "."), "corpus", "composite",
                base + ".go");
        java.util.List<String> vars = ControlFlowCorpusTest.observeVars(src);
        long[] expected = ControlFlowCorpusTest.gcOracle(src, vars);
        long[] actual = ControlFlowCorpusTest.graalgoValues(src, vars);
        if (expected.length != vars.size() || actual.length != vars.size()) {
            throw new AssertionError(base + ": value count mismatch (vars=" + vars.size()
                    + " gc=" + expected.length + " graalgo=" + actual.length + ")");
        }
        for (int i = 0; i < vars.size(); i++) {
            if (expected[i] != actual[i]) {
                throw new AssertionError(base + "." + vars.get(i) + ": gc="
                        + Long.toUnsignedString(expected[i])
                        + " graalgo=" + Long.toUnsignedString(actual[i]));
            }
        }
    }
}
