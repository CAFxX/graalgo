package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import graalgo.launcher.Main;
import graalgo.launcher.ToolPaths;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

/**
 * M2 acceptance: the straight-line corpus, differential vs stock gc.
 *
 * <p>Red-green-first (SPEC §5.2): for each corpus program the expected values
 * come from stock gc at test time — the test cannot pass by construction.
 * The oracle path: inject a {@code println(vars...)} before {@code func main}'s
 * closing brace and before every bare {@code return} (corpus files are
 * gofmt-clean, so the last {@code }}-alone line is that brace), run
 * {@code go run} with {@code GOTOOLCHAIN=local GOMAXPROCS=1}, and parse the
 * first stderr line's space-separated values (Go's {@code println} writes to
 * stderr; at most one injected println executes per run).
 * The graalgo path: {@code gofrontend} →
 * {@link NodeFactory} → execute on a manually created frame → frame-slot
 * reads (the M2 observability idiom: {@code go/types} rejects
 * {@code func main() int}, so program exit values are not observable).
 *
 * <p>Corpus conventions (file list, {@code // observe:} trailer, the
 * {@code z = z} no-op-use idiom) are documented in
 * {@code corpus/straightline/README.md}. Run from the phase1 directory
 * ({@code make test}); override with {@code -Dgraalgo.repo.dir=...}.
 */
// verifies: GG-0012 (M2: gofrontend emits typed AST; NodeFactory builds
// straight-line programs end-to-end; every program's computed values are
// asserted equal to stock gc's output).
public final class StraightLineCorpusTest {

    private static Path repoDir() {
        return Path.of(System.getProperty("graalgo.repo.dir", "."));
    }

    private static Path corpusDir() {
        return repoDir().resolve("test").resolve("corpus").resolve("phase1").resolve("straightline");
    }

    private static List<String> programs() throws Exception {
        try (Stream<Path> s = Files.list(corpusDir())) {
            return s.map(p -> p.getFileName().toString())
                    .filter(n -> n.startsWith("sl") && n.endsWith(".go"))
                    .sorted()
                    .map(n -> n.substring(0, n.length() - 3))
                    .toList();
        }
    }

    /** Guards against accidental corpus shrinkage: the edge-case cover below must stay. */
    public void testCorpusHasEnoughPrograms() throws Exception {
        List<String> ps = programs();
        if (ps.size() < 10) {
            throw new AssertionError(
                    "straight-line corpus shrank to " + ps.size() + " programs: " + ps);
        }
    }

    public void testSl01Consts() throws Exception { checkProgram("sl01_consts"); }
    public void testSl02Arith() throws Exception { checkProgram("sl02_arith"); }
    public void testSl03Bitwise() throws Exception { checkProgram("sl03_bitwise"); }
    public void testSl04Shifts() throws Exception { checkProgram("sl04_shifts"); }
    public void testSl05IntUint() throws Exception { checkProgram("sl05_int_uint"); }
    public void testSl06Multiassign() throws Exception { checkProgram("sl06_multiassign"); }
    public void testSl07Define() throws Exception { checkProgram("sl07_define"); }
    public void testSl08Incdec() throws Exception { checkProgram("sl08_incdec"); }
    public void testSl09Unary() throws Exception { checkProgram("sl09_unary"); }
    public void testSl10Overflow() throws Exception { checkProgram("sl10_overflow"); }
    public void testSl11Divmod() throws Exception { checkProgram("sl11_divmod"); }
    public void testSl12Earlyreturn() throws Exception { checkProgram("sl12_earlyreturn"); }

    /**
     * Runs one corpus program through both backends and asserts every
     * observed variable is bit-identical.
     */
    static void checkProgram(String base) throws Exception {
        Path src = corpusDir().resolve(base + ".go");
        List<String> vars = observeVars(src);
        long[] expected = gcOracle(src, vars);
        long[] actual = graalgoValues(src, vars);
        if (expected.length != vars.size() || actual.length != vars.size()) {
            throw new AssertionError(base + ": value count mismatch (vars=" + vars.size()
                    + " gc=" + expected.length + " graalgo=" + actual.length + ")");
        }
        for (int i = 0; i < vars.size(); i++) {
            if (expected[i] != actual[i]) {
                throw new AssertionError(base + "." + vars.get(i) + ": gc=" + Long.toUnsignedString(expected[i])
                        + " graalgo=" + Long.toUnsignedString(actual[i]));
            }
        }
    }

    /** Reads the {@code // observe: v1 v2 ...} trailer (last line of the file). */
    static List<String> observeVars(Path src) throws Exception {
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        for (int i = lines.size() - 1; i >= 0; i--) {
            String t = lines.get(i).trim();
            if (t.startsWith("// observe:")) {
                List<String> vars = new ArrayList<>();
                for (String w : t.substring("// observe:".length()).trim().split("\\s+")) {
                    if (!w.isEmpty()) {
                        vars.add(w);
                    }
                }
                if (vars.isEmpty()) {
                    throw new AssertionError(src + ": empty // observe: trailer");
                }
                return vars;
            }
        }
        throw new AssertionError(src + ": missing // observe: trailer");
    }

    /** Stock-gc oracle: println-injected variant, {@code go run}, parse stderr. */
    static long[] gcOracle(Path src, List<String> vars) throws Exception {
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        // Injection rule (documented in corpus/straightline/README.md): the
        // println goes before func main's closing brace (last "}"-alone line)
        // and before every bare "return", so early returns are observed too.
        // At most one injected println executes per run; the first stderr
        // line is the oracle output.
        String probe = "\tprintln(" + String.join(", ", vars) + ")";
        List<String> out = new ArrayList<>();
        for (String line : lines) {
            if (line.trim().equals("return")) {
                out.add(probe);
            }
            out.add(line);
        }
        int insertAt = -1;
        for (int i = 0; i < out.size(); i++) {
            if (out.get(i).equals("}")) {
                insertAt = i;
            }
        }
        if (insertAt < 0) {
            throw new AssertionError(src + ": no closing brace for println injection");
        }
        out.add(insertAt, probe);
        Path dir = Files.createTempDirectory("graalgo-oracle");
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
        // println writes to stderr; the first line is the oracle output
        // (a bare return is injected before, so it prints before unwinding).
        String firstLine = null;
        for (String l : stderr.split("\n")) {
            if (!l.trim().isEmpty()) {
                firstLine = l;
                break;
            }
        }
        if (firstLine == null) {
            throw new AssertionError(src + ": oracle printed nothing: stderr=<" + stderr + ">");
        }
        String[] parts = firstLine.trim().split("\\s+");
        if (parts.length != vars.size()) {
            throw new AssertionError(src + ": oracle output shape mismatch: <" + firstLine.trim()
                    + "> for vars " + vars);
        }
        long[] result = new long[parts.length];
        for (int i = 0; i < parts.length; i++) {
            try {
                // println prints unsigned decimal for uint kinds, signed for
                // int kinds; both must parse to the same bit pattern.
                result[i] = parts[i].startsWith("-")
                        ? Long.parseLong(parts[i])
                        : Long.parseUnsignedLong(parts[i]);
            } catch (NumberFormatException e) {
                throw new AssertionError(src + ": oracle value <" + parts[i]
                        + "> is not an integer", e);
            }
        }
        return result;
    }

    /** graalgo path: gofrontend → NodeFactory → execute → frame-slot reads. */
    static long[] graalgoValues(Path src, List<String> vars) throws Exception {
        String json = Main.runGofrontend(src);
        String text = Files.readString(src, StandardCharsets.UTF_8);
        Source source = Source.newBuilder("go", text, src.getFileName().toString()).build();
        NodeFactory.Program program = new NodeFactory(source).buildProgram(json);
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], program.descriptor());
        program.root().execute(frame);
        Map<String, Integer> slots = program.slotsByName();
        long[] out = new long[vars.size()];
        for (int i = 0; i < vars.size(); i++) {
            Integer slot = slots.get(vars.get(i));
            if (slot == null) {
                throw new AssertionError(src + ": no frame slot for observed var <"
                        + vars.get(i) + "> (slots: " + slots.keySet() + ")");
            }
            out[i] = frame.getLong(slot);
        }
        return out;
    }
}
