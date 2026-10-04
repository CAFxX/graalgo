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
 * M6 acceptance: the generics corpus, differential vs stock gc.
 *
 * <p>Red-green-first (SPEC §5.2): for each corpus program the expected values
 * come from stock gc at test time — the test cannot pass by construction.
 * The oracle path and the graalgo path mirror
 * {@link StraightLineCorpusTest}: a {@code println(vars...)} is injected
 * before {@code func main}'s closing brace (corpus files are gofmt-clean),
 * run via {@code go run}, and the first stderr line's space-separated values
 * are compared against graalgo's frame-slot reads. All observed variables
 * are integers (the M2 observability idiom).
 *
 * <p>Corpus conventions: {@code gn*.go} files, {@code // observe:} trailer,
 * the {@code x = x} no-op-use idiom. Every file carries
 * {@code // verifies: GG-0014} (the M6 monomorphization requirement).
 * Run from the phase1 directory ({@code make test}); override with
 * {@code -Dgraalgo.phase1.dir=...}.
 */
// verifies: GG-0014 (M6: monomorphized generics — one RootNode per go/types
// instantiation, functions and methods incl. generic methods — differential
// vs stock gc on the generics corpus).
public final class GenericsCorpusTest {

    private static Path phase1Dir() {
        return Path.of(System.getProperty("graalgo.phase1.dir", "."));
    }

    private static Path corpusDir() {
        return phase1Dir().resolve("corpus").resolve("generics");
    }

    private static List<String> programs() throws Exception {
        try (Stream<Path> s = Files.list(corpusDir())) {
            return s.map(p -> p.getFileName().toString())
                    .filter(n -> n.startsWith("gn") && n.endsWith(".go"))
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
                    "generics corpus shrank to " + ps.size() + " programs: " + ps);
        }
    }

    public void testGn01NamedFuncs() throws Exception { checkProgram("gn01_named_funcs"); }
    public void testGn02GenericFuncs() throws Exception { checkProgram("gn02_generic_funcs"); }
    public void testGn03GenericMethods() throws Exception { checkProgram("gn03_generic_methods"); }
    public void testGn04GenericMethodTargs() throws Exception { checkProgram("gn04_generic_method_targs"); }
    public void testGn05BoundMethod() throws Exception { checkProgram("gn05_bound_method"); }
    public void testGn06NestedInst() throws Exception { checkProgram("gn06_nested_inst"); }
    public void testGn07HigherOrder() throws Exception { checkProgram("gn07_higher_order"); }
    public void testGn08Constraints() throws Exception { checkProgram("gn08_constraints"); }
    public void testGn09FmtOverlay() throws Exception { checkProgram("gn09_fmt_overlay"); }
    public void testGn10MethodrefBox() throws Exception { checkProgram("gn10_methodref_box"); }

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
